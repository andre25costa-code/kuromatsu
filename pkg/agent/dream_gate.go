package agent

import (
	"context"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

// coldPathRunnable is the method set evolution.Runtime and sleep.Runtime
// both already implement -- the same shape as evolution's own unexported
// coldPathRuntime interface that NewColdPathRunnerWithErrorHandler expects
// (Go satisfies unexported interface parameters structurally, so dreamGate
// doesn't need to import that private type to be accepted there).
type coldPathRunnable interface {
	RunColdPathOnce(ctx context.Context, workspace string) error
}

// defaultDreamRetriggerAfter is how long a busy-refused dream run waits
// before retrying (ADR-016/S17's "evolução re-tenta em ~2 min").
const defaultDreamRetriggerAfter = 2 * time.Minute

// dreamGate wraps a coldPathRunnable (evolution's Runtime for evolução,
// sleep's Runtime for the modo dormir bridge) so it only ever runs inside
// runstate's Dream bit (ADR-016 point 7): every call first goes through
// TryEnterDream, which refuses (runstate.ErrBusy) unless the engine is
// exactly Idle, and whose ctx is canceled the instant a user Inference
// begins -- Inference always preempts Dream (S09), never the other way
// around.
type dreamGate struct {
	inner coldPathRunnable
	rs    *runstate.Engine

	// retrigger re-queues workspace after a busy refusal (evolution.
	// ColdPathRunner.Trigger, or the sleep scheduler's own retry hook) so
	// a run that lost to a user turn is actually retried instead of
	// waiting for the next scheduled/explicit trigger. Set by the
	// constructor right after building whatever owns this gate --
	// necessarily after dreamGate itself, since the runner needs the gate
	// to exist first. nil is a safe no-op, used by tests that only care
	// about the gating itself.
	retrigger func(workspace string) bool
	// retriggerAfter overrides defaultDreamRetriggerAfter (tests only);
	// zero means use the default.
	retriggerAfter time.Duration
}

func (g *dreamGate) RunColdPathOnce(parent context.Context, workspace string) error {
	if g.rs == nil {
		// runstate.enabled=false (the default): ungated passthrough, the
		// exact pre-Trilho-C behavior ("evolução igual" -- see
		// runstateEngineForConfig's doc comment).
		return g.inner.RunColdPathOnce(parent, workspace)
	}

	dreamCtx, release, ok := g.rs.TryEnterDream(parent)
	if !ok {
		if g.retrigger != nil {
			after := g.retriggerAfter
			if after <= 0 {
				after = defaultDreamRetriggerAfter
			}
			time.AfterFunc(after, func() { g.retrigger(workspace) })
		}
		return runstate.ErrBusy
	}
	defer release()
	return g.inner.RunColdPathOnce(dreamCtx, workspace)
}
