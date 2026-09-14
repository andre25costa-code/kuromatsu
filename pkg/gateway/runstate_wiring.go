package gateway

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/agent"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// installRunstateIntegration wires the Trilho C SO integration
// (ADR-016/FR-017) onto the currently-running services: the run/state
// file, log, and sd_notify publishers; the heartbeat skip-when-busy hook
// (AC-017-2); and /health's aditive "state" field (AC-017-1). Called once
// at initial gateway startup (setupAndStartServices) and again after every
// config reload (restartServices), since either can change
// cfg.Runstate.Enabled or which *HeartbeatService the hook needs
// installing on.
//
// With runstate.enabled=false (the default), this explicitly clears any
// previously-installed hooks and starts no goroutine -- so a live reload
// that flips the flag off is honored immediately, and AC-017-7 holds
// exactly (not just "true until the first reload").
func installRunstateIntegration(cfg *config.Config, al *agent.AgentLoop, runningServices *services) {
	if runningServices.runstatePublishersStop != nil {
		runningServices.runstatePublishersStop()
		runningServices.runstatePublishersStop = nil
	}

	rs := al.Runstate()
	if rs == nil {
		if runningServices.HeartbeatService != nil {
			runningServices.HeartbeatService.SetShouldSkip(nil)
		}
		if runningServices.HealthServer != nil {
			runningServices.HealthServer.SetStateFunc(nil)
		}
		return
	}

	if runningServices.HealthServer != nil {
		runningServices.HealthServer.SetStateFunc(func() (uint32, []string) {
			mode := rs.Snapshot()
			return uint32(mode), mode.Names()
		})
	}

	if runningServices.HeartbeatService != nil {
		if cfg.Runstate.EffectiveSkipHeartbeatWhenBusy() {
			runningServices.HeartbeatService.SetShouldSkip(func() bool {
				return rs.Snapshot().Any(runstate.Inference | runstate.ToolExec | runstate.Dream)
			})
		} else {
			runningServices.HeartbeatService.SetShouldSkip(nil)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	runningServices.runstatePublishersStop = cancel
	go runstate.RunFilePublisher(ctx, rs, cfg.Runstate.EffectiveStateFile(config.GetHome()))
	go runstate.RunLogPublisher(ctx, rs)
	if cfg.Runstate.EffectiveSdNotify() {
		go runstate.RunSdNotifyPublisher(ctx, rs)
		// A no-op unless the unit itself sets WatchdogSec (WATCHDOG_USEC in
		// the environment) -- see RunSdWatchdogPinger's doc comment for why
		// this needs no separate config knob or runstate.enabled gate of
		// its own beyond sd_notify being on.
		go runstate.RunSdWatchdogPinger(ctx)
	}
	// AC-017-6: the CPU-credit (steal%) watchdog that toggles the purely
	// informative Throttled bit. Shares runstatePublishersStop's lifecycle
	// exactly like the three publishers above -- no separate config knob,
	// since Throttled has no gate of its own to be worth decoupling from
	// runstate.enabled (ADR-016 point 2).
	go runstate.RunThrottleWatchdog(ctx, rs, sysinfo.CPUStatTotal)
}

// sdReadyOnStartup sends sd_notify READY=1, unconditionally -- deliberately
// NOT gated on cfg.Runstate (unlike installRunstateIntegration above): the
// deploy unit's Type=notify makes this a systemd contract the process must
// honor regardless of whether the optional runstate feature is on, or
// systemd leaves the unit stuck "activating" until TimeoutStartSec kills
// it. SdNotify itself is already a safe no-op with no NOTIFY_SOCKET (not
// running under systemd, or a unit without NotifyAccess) or on platforms
// without unixgram sockets. Called once, right after the gateway is fully
// up (boot only -- never on reload, matching systemd's own "the unit only
// starts once" semantics).
func sdReadyOnStartup() {
	if err := runstate.SdReady(); err != nil {
		logger.DebugCF("gateway", "sd_notify READY=1 failed", map[string]any{"error": err.Error()})
	}
}

// sdStoppingOnShutdown sends sd_notify STOPPING=1, unconditionally (see
// sdReadyOnStartup for why). Called once, at the start of a full (not
// reload) shutdown.
func sdStoppingOnShutdown() {
	if err := runstate.SdStopping(); err != nil {
		logger.DebugCF("gateway", "sd_notify STOPPING=1 failed", map[string]any{"error": err.Error()})
	}
}
