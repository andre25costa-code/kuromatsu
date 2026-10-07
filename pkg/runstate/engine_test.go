package runstate

import (
	"context"
	"testing"
	"time"
)

func TestMode_StringAndNames(t *testing.T) {
	if got := Idle.String(); got != "idle" {
		t.Fatalf("Idle.String() = %q, want %q", got, "idle")
	}
	if got := len(Idle.Names()); got != 0 {
		t.Fatalf("Idle.Names() len = %d, want 0", got)
	}

	combo := Inference | ToolExec
	if got, want := combo.String(), "inference,toolexec"; got != want {
		t.Fatalf("combo.String() = %q, want %q", got, want)
	}
	if !combo.Has(Inference) || !combo.Has(ToolExec) {
		t.Fatalf("combo.Has(...) = false, want true for both bits")
	}
	if combo.Has(Dream) {
		t.Fatalf("combo.Has(Dream) = true, want false")
	}
}

func TestEngine_IncDec_Refcounted(t *testing.T) {
	e := New()

	release1 := e.Enter(ToolExec)
	if !e.Snapshot().Has(ToolExec) {
		t.Fatalf("ToolExec not active after first Enter")
	}
	release2 := e.Enter(ToolExec)

	release1()
	if !e.Snapshot().Has(ToolExec) {
		t.Fatalf("ToolExec cleared after only one of two releases -- refcount broken")
	}

	release2()
	if e.Snapshot().Has(ToolExec) {
		t.Fatalf("ToolExec still active after both releases")
	}
}

func TestEngine_Enter_ReleaseIsIdempotent(t *testing.T) {
	e := New()
	release := e.Enter(Reflex)
	release()
	release() // must not double-Dec (which would floor at 0 anyway, but must not panic or corrupt other bits)
	if e.Snapshot() != Idle {
		t.Fatalf("Snapshot() = %v, want Idle after idempotent double release", e.Snapshot())
	}
}

func TestEngine_Dec_NeverGoesNegative(t *testing.T) {
	e := New()
	e.Dec(Inference) // unmatched Dec, no prior Inc
	if e.Snapshot().Has(Inference) {
		t.Fatalf("unmatched Dec set a bit that was never Inc'd")
	}
	e.Inc(Inference)
	if !e.Snapshot().Has(Inference) {
		t.Fatalf("Inc after an unmatched Dec should still work")
	}
}

func TestEngine_TryEnterDream_SucceedsOnlyWhenIdle(t *testing.T) {
	e := New()

	ctx, release, ok := e.TryEnterDream(context.Background())
	if !ok {
		t.Fatalf("TryEnterDream on a fresh Idle engine: ok = false, want true")
	}
	if !e.Snapshot().Has(Dream) {
		t.Fatalf("Dream not active after successful TryEnterDream")
	}
	if ctx.Err() != nil {
		t.Fatalf("dream ctx already done: %v", ctx.Err())
	}

	// A second attempt must be refused: Dream itself counts as "not Idle".
	_, _, ok2 := e.TryEnterDream(context.Background())
	if ok2 {
		t.Fatalf("second concurrent TryEnterDream succeeded, want refused (single-flight)")
	}

	release()
	if e.Snapshot() != Idle {
		t.Fatalf("Snapshot() = %v after release, want Idle", e.Snapshot())
	}

	// Idle again: a fresh attempt must succeed.
	_, release3, ok3 := e.TryEnterDream(context.Background())
	if !ok3 {
		t.Fatalf("TryEnterDream after release: ok = false, want true")
	}
	release3()
}

func TestEngine_TryEnterDream_RefusedWhenBusy(t *testing.T) {
	e := New()
	releaseInference := e.Enter(Inference)
	defer releaseInference()

	_, _, ok := e.TryEnterDream(context.Background())
	if ok {
		t.Fatalf("TryEnterDream succeeded while Inference active, want refused")
	}
}

func TestEngine_TryEnterDream_RefusedWhenSuspended(t *testing.T) {
	e := New()
	e.Suspend()
	defer e.Resume()

	_, _, ok := e.TryEnterDream(context.Background())
	if ok {
		t.Fatalf("TryEnterDream succeeded while Suspended, want refused")
	}
}

func TestEngine_IncInference_PreemptsDream(t *testing.T) {
	e := New()
	dreamCtx, release, ok := e.TryEnterDream(context.Background())
	if !ok {
		t.Fatalf("TryEnterDream: ok = false, want true")
	}
	defer release()

	select {
	case <-dreamCtx.Done():
		t.Fatalf("dream ctx already done before any Inference")
	default:
	}

	releaseInference := e.Enter(Inference)
	defer releaseInference()

	select {
	case <-dreamCtx.Done():
		// expected: Inference preempts Dream
	case <-time.After(time.Second):
		t.Fatalf("dream ctx not canceled after Inc(Inference) -- preemption did not fire")
	}
}

func TestEngine_TryEnter_RefusedWhenSuspendedInference(t *testing.T) {
	e := New()
	e.Suspend()
	defer e.Resume()

	release, ok := e.TryEnter(Inference)
	if ok {
		t.Fatalf("TryEnter(Inference) succeeded while Suspended, want refused")
	}
	if e.Snapshot().Has(Inference) {
		t.Fatalf("Inference bit set despite a refused TryEnter")
	}
	// release must be a safe no-op even though ok=false.
	release()
	if e.Snapshot() != Suspended {
		t.Fatalf("Snapshot() = %v after no-op release, want just Suspended", e.Snapshot())
	}
}

func TestEngine_TryEnter_RefusedWhenSuspendedToolExec(t *testing.T) {
	e := New()
	e.Suspend()
	defer e.Resume()

	release, ok := e.TryEnter(ToolExec)
	if ok {
		t.Fatalf("TryEnter(ToolExec) succeeded while Suspended, want refused")
	}
	if e.Snapshot().Has(ToolExec) {
		t.Fatalf("ToolExec bit set despite a refused TryEnter")
	}
	release()
}

func TestEngine_TryEnter_SucceedsWhenNotSuspended(t *testing.T) {
	e := New()

	release, ok := e.TryEnter(Inference)
	if !ok {
		t.Fatalf("TryEnter(Inference) refused on a fresh Idle engine, want success")
	}
	if !e.Snapshot().Has(Inference) {
		t.Fatalf("Inference not active after successful TryEnter")
	}

	// Concurrent holders of the same bit (and of ToolExec alongside it) are
	// normal and must keep working -- only Suspended vetoes a new entry.
	release2, ok2 := e.TryEnter(Inference)
	if !ok2 {
		t.Fatalf("second concurrent TryEnter(Inference) refused, want success (not Idle-gated like Dream)")
	}
	releaseTool, okTool := e.TryEnter(ToolExec)
	if !okTool {
		t.Fatalf("TryEnter(ToolExec) refused alongside Inference, want success")
	}

	release()
	release2()
	releaseTool()
	if e.Snapshot() != Idle {
		t.Fatalf("Snapshot() = %v after all releases, want Idle", e.Snapshot())
	}
}

func TestEngine_TryEnter_ReleaseIsIdempotentAndDecsExactlyOnce(t *testing.T) {
	e := New()
	release, ok := e.TryEnter(ToolExec)
	if !ok {
		t.Fatalf("TryEnter(ToolExec) refused, want success")
	}
	release2, ok2 := e.TryEnter(ToolExec)
	if !ok2 {
		t.Fatalf("second TryEnter(ToolExec) refused, want success")
	}

	release()
	release() // idempotent: must not double-Dec
	if !e.Snapshot().Has(ToolExec) {
		t.Fatalf("ToolExec cleared after only one of two holders released")
	}
	release2()
	if e.Snapshot().Has(ToolExec) {
		t.Fatalf("ToolExec still active after both holders released")
	}
}

func TestEngine_TryEnter_ResumeAllowsNewEntryAgain(t *testing.T) {
	e := New()
	e.Suspend()
	if _, ok := e.TryEnter(Inference); ok {
		t.Fatalf("TryEnter(Inference) succeeded while Suspended, want refused")
	}
	e.Resume()

	release, ok := e.TryEnter(Inference)
	if !ok {
		t.Fatalf("TryEnter(Inference) refused after Resume, want success")
	}
	release()
}

func TestEngine_TryEnterInference_PreemptsDream(t *testing.T) {
	e := New()
	dreamCtx, releaseDream, ok := e.TryEnterDream(context.Background())
	if !ok {
		t.Fatalf("TryEnterDream: ok = false, want true")
	}
	defer releaseDream()

	release, ok := e.TryEnter(Inference)
	if !ok {
		t.Fatalf("TryEnter(Inference) refused, want success")
	}
	defer release()

	select {
	case <-dreamCtx.Done():
		// expected: Inference preempts Dream, same as Inc(Inference)
	case <-time.After(time.Second):
		t.Fatalf("dream ctx not canceled after TryEnter(Inference) -- preemption did not fire")
	}
}

func TestEngine_SuspendResume(t *testing.T) {
	e := New()
	if e.Snapshot().Has(Suspended) {
		t.Fatalf("Suspended set before Suspend() called")
	}
	e.Suspend()
	if !e.Snapshot().Has(Suspended) {
		t.Fatalf("Suspended not set after Suspend()")
	}
	e.Suspend() // idempotent
	if !e.Snapshot().Has(Suspended) {
		t.Fatalf("Suspended cleared by a second Suspend() call")
	}
	e.Resume()
	if e.Snapshot().Has(Suspended) {
		t.Fatalf("Suspended still set after Resume()")
	}
}

func TestEngine_Since_UpdatesOnTransitionOnly(t *testing.T) {
	e := New()
	t0 := e.Since()

	time.Sleep(2 * time.Millisecond)
	release := e.Enter(ToolExec)
	t1 := e.Since()
	if !t1.After(t0) {
		t.Fatalf("Since() did not advance after a real transition")
	}

	// A second, concurrent Enter of the SAME bit is not a transition (the
	// aggregate Mode doesn't change) -- Since() must not move again.
	release2 := e.Enter(ToolExec)
	t2 := e.Since()
	if !t2.Equal(t1) {
		t.Fatalf("Since() advanced on a non-transition (concurrent Enter of the same bit)")
	}
	release()
	release2()
}

func TestEngine_Subscribe_SeedsCurrentStateAndReportsTransitions(t *testing.T) {
	e := New()
	ch, cancel := e.Subscribe()
	defer cancel()

	select {
	case mode := <-ch:
		if mode != Idle {
			t.Fatalf("seeded value = %v, want Idle", mode)
		}
	case <-time.After(time.Second):
		t.Fatalf("Subscribe did not seed the current state")
	}

	release := e.Enter(Reflex)
	defer release()

	select {
	case mode := <-ch:
		if !mode.Has(Reflex) {
			t.Fatalf("transition value = %v, want Reflex set", mode)
		}
	case <-time.After(time.Second):
		t.Fatalf("Subscribe did not report the Enter(Reflex) transition")
	}
}

func TestEngine_Default_ReturnsSameInstance(t *testing.T) {
	if Default() != Default() {
		t.Fatalf("Default() returned two different instances")
	}
}

func TestEngine_New_IsIndependentOfDefault(t *testing.T) {
	fresh := New()
	if fresh == Default() {
		t.Fatalf("New() returned the Default() singleton")
	}
}
