package agent

import (
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

// runstateEngineForConfig resolves the *runstate.Engine an AgentLoop should
// wire into its hooks (activeRequestsInc/Dec, askSideQuestion,
// ExecuteTools, tryReflex, the evolution/sleep dreamGates) for cfg: nil
// when runstate.enabled=false (ADR-016's compat promise -- every hook
// treats nil as "do nothing"), otherwise the single process-wide
// runstate.Default() instance. Never runstate.New(): a fresh instance
// wired in here would silently stop sharing state with gateway.go's
// publishers/heartbeat-skip installation, which always target Default().
func runstateEngineForConfig(cfg *config.Config) *runstate.Engine {
	if cfg == nil || !cfg.Runstate.Enabled {
		return nil
	}
	return runstate.Default()
}

// rsSnapshot returns the AgentLoop's current runstate engine (nil when
// disabled), guarded by mu like focus/reflexes.
func (al *AgentLoop) rsSnapshot() *runstate.Engine {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.runstate
}

// Runstate is the exported form of rsSnapshot, for wiring code outside
// pkg/agent (gateway.go's publishers/heartbeat-skip/health-state
// installation, C2) that needs the same engine every in-process hook
// already targets. nil when runstate.enabled=false.
func (al *AgentLoop) Runstate() *runstate.Engine {
	return al.rsSnapshot()
}

// rsEnter is Enter(bit) against al's current runstate engine, or a safe
// no-op release when runstate is disabled. Callers use it exactly like a
// bare Engine.Enter: `release := al.rsEnter(runstate.ToolExec); defer release()`.
//
// rsEnter never refuses -- only Reflex (S16/FR-013) uses it today, and
// Reflex is deliberately outside Suspended's veto (S09/ADR-016 point 6
// names only Inference/ToolExec/Dream). Inference/ToolExec call sites use
// rsTryEnter below instead.
func (al *AgentLoop) rsEnter(bit runstate.Mode) func() {
	rs := al.rsSnapshot()
	if rs == nil {
		return func() {}
	}
	return rs.Enter(bit)
}

// rsTryEnter is TryEnter(bit) against al's current runstate engine: it
// refuses when Suspended is active (S09/ADR-016 point 6 -- "enquanto
// Suspended está ativo, nenhuma nova entrada de Inference/ToolExec/Dream é
// aceita"). When runstate is disabled (nil engine) it always succeeds with
// a safe no-op release, exactly like rsEnter -- there is no Suspended bit
// to check. Used by the Inference/ToolExec call sites the invariant
// actually names (askSideQuestion, ExecuteTools, activeRequestsInc);
// Reflex/Dream have their own gates (rsEnter, TryEnterDream) and must not
// go through here.
func (al *AgentLoop) rsTryEnter(bit runstate.Mode) (release func(), ok bool) {
	rs := al.rsSnapshot()
	if rs == nil {
		return func() {}, true
	}
	return rs.TryEnter(bit)
}
