package gateway

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/agent"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/localllm"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

// installMemguardIntegration wires the Trilho C memory guard (ADR-017/
// FR-018) onto the native engine's pre/postLoad hooks and starts (or
// restarts) its PSI watchdog goroutine. Called once at initial gateway
// startup and again after every config reload -- mirrors
// installRunstateIntegration's shape exactly, including why: a live
// reload that flips memguard.enabled off must actually clear the hooks
// and stop the watchdog, not just leave the previous ones running
// (AC-018-6 spirit extended to "disabled at runtime", not just "disabled
// at boot").
//
// guard.rs (the runstate Engine memguard Suspends/Resumes) is
// al.Runstate(), which may itself be nil (runstate.enabled=false) --
// Guard tolerates that via its own local suspended-state fallback (see
// runstate.Guard's doc comment), so memguard.enabled and runstate.enabled
// are independent switches, exactly as their separate config blocks imply.
func installMemguardIntegration(cfg *config.Config, al *agent.AgentLoop, runningServices *services) {
	if runningServices.memguardStop != nil {
		runningServices.memguardStop()
		runningServices.memguardStop = nil
	}

	if !cfg.Memguard.Enabled {
		localllm.SetMemoryHooks(nil, nil)
		return
	}

	guardCfg := runstate.GuardConfig{
		GOGC:             cfg.Memguard.EffectiveGOGC(),
		MarginMB:         cfg.Memguard.EffectiveMarginMB(),
		ComputeBufferMB:  cfg.Memguard.EffectiveComputeBufferMB(),
		MinAvailableMB:   cfg.Memguard.EffectiveMinAvailableMB(),
		PSISomeThreshold: cfg.Memguard.EffectivePSISomeThreshold(),
		PSIFullThreshold: cfg.Memguard.EffectivePSIFullThreshold(),
		PSISustainSecs:   cfg.Memguard.EffectivePSISustainSecs(),
	}
	guard := runstate.NewGuard(al.Runstate(), guardCfg, localllm.UnloadAll)

	localllm.SetMemoryHooks(
		func(est localllm.MemoryEstimate) error {
			return guard.PreLoad(est.KVBytes, est.ComputeBytes)
		},
		func(est localllm.MemoryEstimate) {
			guard.ApplyPlan(est.ModelBytes, est.KVBytes)
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	runningServices.memguardStop = cancel
	go guard.Run(ctx)
}
