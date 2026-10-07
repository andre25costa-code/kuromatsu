package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

type fakeColdPathRunnable struct {
	mu       sync.Mutex
	calls    int
	lastCtx  context.Context
	lastWork string
	err      error
}

func (f *fakeColdPathRunnable) RunColdPathOnce(ctx context.Context, workspace string) error {
	f.mu.Lock()
	f.calls++
	f.lastCtx = ctx
	f.lastWork = workspace
	f.mu.Unlock()
	return f.err
}

func (f *fakeColdPathRunnable) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestDreamGate_NilEngineIsUngatedPassthrough covers ADR-016's "evolução
// igual" compat promise: with runstate.enabled=false, newEvolutionBridge
// wires rs=nil into dreamGate, and this must behave exactly like calling
// inner.RunColdPathOnce directly -- no refusal, ever, regardless of what
// else is "active" (there is no engine to check).
func TestDreamGate_NilEngineIsUngatedPassthrough(t *testing.T) {
	inner := &fakeColdPathRunnable{}
	gate := &dreamGate{inner: inner, rs: nil}

	if err := gate.RunColdPathOnce(context.Background(), "ws"); err != nil {
		t.Fatalf("RunColdPathOnce with nil rs: %v, want nil", err)
	}
	if inner.callCount() != 1 {
		t.Fatalf("inner.calls = %d, want 1", inner.callCount())
	}
}

func TestDreamGate_RunsInnerWhenIdle(t *testing.T) {
	rs := runstate.New()
	inner := &fakeColdPathRunnable{}
	gate := &dreamGate{inner: inner, rs: rs}

	if err := gate.RunColdPathOnce(context.Background(), "ws"); err != nil {
		t.Fatalf("RunColdPathOnce: %v", err)
	}
	if inner.callCount() != 1 {
		t.Fatalf("inner.calls = %d, want 1", inner.callCount())
	}
	if rs.Snapshot() != runstate.Idle {
		t.Fatalf("Snapshot() = %v after RunColdPathOnce returned, want Idle (release must fire)", rs.Snapshot())
	}
}

func TestDreamGate_RefusedWhenBusy(t *testing.T) {
	rs := runstate.New()
	release := rs.Enter(runstate.Inference)
	defer release()

	inner := &fakeColdPathRunnable{}
	gate := &dreamGate{inner: inner, rs: rs}

	err := gate.RunColdPathOnce(context.Background(), "ws")
	if !errors.Is(err, runstate.ErrBusy) {
		t.Fatalf("RunColdPathOnce err = %v, want runstate.ErrBusy", err)
	}
	if inner.callCount() != 0 {
		t.Fatalf("inner.calls = %d, want 0 (busy must never touch inner)", inner.callCount())
	}
}

func TestDreamGate_RetriggersAfterBusyRefusal(t *testing.T) {
	rs := runstate.New()
	release := rs.Enter(runstate.Inference)

	var mu sync.Mutex
	var retriggered string
	done := make(chan struct{})
	gate := &dreamGate{
		inner: &fakeColdPathRunnable{},
		rs:    rs,
		retrigger: func(workspace string) bool {
			mu.Lock()
			retriggered = workspace
			mu.Unlock()
			close(done)
			return true
		},
		retriggerAfter: 10 * time.Millisecond,
	}

	if err := gate.RunColdPathOnce(context.Background(), "my-ws"); !errors.Is(err, runstate.ErrBusy) {
		t.Fatalf("RunColdPathOnce err = %v, want runstate.ErrBusy", err)
	}
	release() // no longer busy, but the retrigger fires unconditionally on its own timer

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("retrigger was never called after the busy refusal")
	}
	mu.Lock()
	defer mu.Unlock()
	if retriggered != "my-ws" {
		t.Fatalf("retrigger workspace = %q, want %q", retriggered, "my-ws")
	}
}

func TestDreamGate_PreemptedByInference(t *testing.T) {
	rs := runstate.New()
	ctxSeen := make(chan context.Context, 1)
	inner := &blockingColdPathRunnable{ctxSeen: ctxSeen}
	gate := &dreamGate{inner: inner, rs: rs}

	runErr := make(chan error, 1)
	go func() { runErr <- gate.RunColdPathOnce(context.Background(), "ws") }()

	var dreamCtx context.Context
	select {
	case dreamCtx = <-ctxSeen:
	case <-time.After(2 * time.Second):
		t.Fatalf("inner.RunColdPathOnce was never invoked")
	}

	release := rs.Enter(runstate.Inference)
	defer release()

	select {
	case <-dreamCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("dream ctx not canceled after Inc(Inference) -- preemption did not propagate through dreamGate")
	}

	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunColdPathOnce never returned after preemption")
	}
}

// blockingColdPathRunnable reports the ctx it was called with and blocks
// until that ctx is done, mimicking a real cold-path run that respects
// cancellation.
type blockingColdPathRunnable struct {
	ctxSeen chan context.Context
}

func (b *blockingColdPathRunnable) RunColdPathOnce(ctx context.Context, workspace string) error {
	b.ctxSeen <- ctx
	<-ctx.Done()
	return ctx.Err()
}
