package runstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

func fakeMemInfo(total, available uint64) func() (uint64, uint64, error) {
	return func() (uint64, uint64, error) { return total, available, nil }
}

func failingMemInfo() (uint64, uint64, error) {
	return 0, 0, errors.New("no /proc/meminfo on this platform")
}

func noCgroup() (uint64, uint64, bool) { return 0, 0, false }

func newTestGuard(cfg GuardConfig, rs *Engine) *Guard {
	g := NewGuard(rs, cfg, nil)
	return g
}

func TestGuard_Plan_ArithmeticAndClamp(t *testing.T) {
	g := newTestGuard(GuardConfig{MarginMB: 96, ComputeBufferMB: 96}, nil)
	g.memInfo = fakeMemInfo(969*mib, 0)
	g.cgroupMemory = noCgroup

	// 969 - 237 (model) - 119 (kv) - 96 (compute) - 96 (margin) ~= 420 MB,
	// mirroring the demetrius numbers in ADR-017's Consequences section.
	got := g.Plan(237*mib, 119*mib)
	want := uint64(969-237-119-96-96) * mib
	if got != want {
		t.Fatalf("Plan() = %d MiB, want %d MiB", got/mib, want/mib)
	}
}

func TestGuard_Plan_ClampsToMinimum(t *testing.T) {
	g := newTestGuard(GuardConfig{MarginMB: 96, ComputeBufferMB: 96}, nil)
	g.memInfo = fakeMemInfo(300*mib, 0) // tiny box: everything would go negative
	g.cgroupMemory = noCgroup

	got := g.Plan(237*mib, 119*mib)
	if got != 128*mib {
		t.Fatalf("Plan() = %d MiB, want the 128 MiB floor", got/mib)
	}
}

func TestGuard_Plan_PrefersCgroupLimitWhenSmaller(t *testing.T) {
	g := newTestGuard(GuardConfig{}, nil)
	g.memInfo = fakeMemInfo(2000*mib, 0)
	g.cgroupMemory = func() (uint64, uint64, bool) { return 0, 850 * mib, true }

	got := g.Plan(0, 0)
	if got != 850*mib {
		t.Fatalf("Plan() = %d MiB, want the cgroup limit (850 MiB), not MemTotal (2000 MiB)", got/mib)
	}
}

func TestGuard_Plan_ZeroCgroupLimitMeansUnbounded(t *testing.T) {
	g := newTestGuard(GuardConfig{}, nil)
	g.memInfo = fakeMemInfo(1000*mib, 0)
	g.cgroupMemory = func() (uint64, uint64, bool) { return 500 * mib, 0, true } // "max", i.e. no limit

	got := g.Plan(0, 0)
	if got != 1000*mib {
		t.Fatalf("Plan() = %d MiB, want MemTotal (1000 MiB) when the cgroup reports no limit", got/mib)
	}
}

func TestGuard_Plan_NoMemInfoReturnsZero(t *testing.T) {
	g := newTestGuard(GuardConfig{}, nil)
	g.memInfo = failingMemInfo

	if got := g.Plan(1, 1); got != 0 {
		t.Fatalf("Plan() = %d, want 0 when MemInfo is unavailable (AC-018-6)", got)
	}
}

func TestGuard_ApplyPlan_NoOpWhenPlanIsZero(t *testing.T) {
	g := newTestGuard(GuardConfig{GOGC: 50}, nil)
	g.memInfo = failingMemInfo

	if got := g.ApplyPlan(1, 1); got != 0 {
		t.Fatalf("ApplyPlan() = %d, want 0 (no MemInfo -> no-op, GOMEMLIMIT untouched)", got)
	}
}

func TestGuard_PreLoad_DeniesWhenBelowFloor(t *testing.T) {
	g := newTestGuard(GuardConfig{MinAvailableMB: 256}, nil)
	g.memInfo = fakeMemInfo(1000*mib, 100*mib) // far below kv+compute+256MB

	err := g.PreLoad(50*mib, 50*mib)
	if !errors.Is(err, ErrOverloaded) {
		t.Fatalf("PreLoad() = %v, want ErrOverloaded", err)
	}
}

func TestGuard_PreLoad_AllowsWhenAboveFloor(t *testing.T) {
	g := newTestGuard(GuardConfig{MinAvailableMB: 256}, nil)
	g.memInfo = fakeMemInfo(1000*mib, 500*mib)

	if err := g.PreLoad(50*mib, 50*mib); err != nil {
		t.Fatalf("PreLoad() = %v, want nil", err)
	}
}

func TestGuard_PreLoad_NoMemInfoAlwaysAllows(t *testing.T) {
	g := newTestGuard(GuardConfig{MinAvailableMB: 999999}, nil)
	g.memInfo = failingMemInfo

	if err := g.PreLoad(0, 0); err != nil {
		t.Fatalf("PreLoad() = %v, want nil when MemInfo is unavailable (AC-018-6)", err)
	}
}

// fakeClock lets pollOnce's "sustained for N seconds" logic be driven
// deterministically instead of racing a real ticker.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestGuard_PollOnce_GCAtMostOncePerMinute(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	gcCalls := 0
	g := newTestGuard(GuardConfig{PSISomeThreshold: 20, PSIFullThreshold: 10, PSISustainSecs: 30}, nil)
	g.now = clock.now
	g.memInfo = fakeMemInfo(1000*mib, 500*mib)
	g.psi = func(string) (sysinfo.Pressure, error) {
		gcCalls++ // not the real GC trigger count, just to prove psi is polled
		return sysinfo.Pressure{SomeAvg10: 50}, nil
	}

	g.pollOnce()
	firstGCTime := g.lastGC
	if firstGCTime.IsZero() {
		t.Fatal("lastGC was never set after pollOnce with SomeAvg10 above threshold")
	}

	clock.advance(10 * time.Second)
	g.pollOnce()
	if !g.lastGC.Equal(firstGCTime) {
		t.Fatal("a second GC ran within the 1-minute cooldown (AC-018-3)")
	}

	clock.advance(time.Minute)
	g.pollOnce()
	if g.lastGC.Equal(firstGCTime) {
		t.Fatal("GC never ran again after the cooldown elapsed")
	}
}

func TestGuard_PollOnce_SuspendsAfterSustainedFullPressureAndCallsUnload(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	unloadCalls := 0
	rs := New()
	g := NewGuard(rs, GuardConfig{PSIFullThreshold: 10, PSISustainSecs: 30}, func() { unloadCalls++ })
	g.now = clock.now
	g.memInfo = fakeMemInfo(1000*mib, 500*mib)
	g.psi = func(string) (sysinfo.Pressure, error) { return sysinfo.Pressure{FullAvg10: 50}, nil }

	g.pollOnce() // first tick above threshold: starts the sustain window, does not suspend yet
	if g.suspended() {
		t.Fatal("suspended before the sustain window elapsed")
	}
	if unloadCalls != 0 {
		t.Fatal("unload called before the sustain window elapsed")
	}

	clock.advance(29 * time.Second)
	g.pollOnce()
	if g.suspended() {
		t.Fatal("suspended one second before the 30s sustain window elapses")
	}

	clock.advance(2 * time.Second) // now 31s since fullSince
	g.pollOnce()
	if !g.suspended() {
		t.Fatal("not suspended after sustained full pressure exceeded PSISustainSecs")
	}
	if !rs.Snapshot().Has(Suspended) {
		t.Fatal("runstate.Suspended bit not set after Guard suspended")
	}
	if unloadCalls != 1 {
		t.Fatalf("unloadCalls = %d, want 1", unloadCalls)
	}
}

func TestGuard_PollOnce_ResumesAfterSustainedRecovery(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	rs := New()
	g := NewGuard(rs, GuardConfig{PSIFullThreshold: 10, PSISustainSecs: 30}, nil)
	g.now = clock.now
	g.memInfo = fakeMemInfo(1000*mib, 500*mib)

	pressure := sysinfo.Pressure{FullAvg10: 50}
	g.psi = func(string) (sysinfo.Pressure, error) { return pressure, nil }
	g.pollOnce()
	clock.advance(31 * time.Second)
	g.pollOnce()
	if !g.suspended() {
		t.Fatal("setup failed: guard never suspended")
	}

	pressure = sysinfo.Pressure{FullAvg10: 1} // below resumeFullThreshold (5)
	g.pollOnce()
	if !g.suspended() {
		t.Fatal("suspended flipped off before the resume sustain window elapsed")
	}

	clock.advance(31 * time.Second)
	g.pollOnce()
	if g.suspended() {
		t.Fatal("still suspended after sustained recovery exceeded PSISustainSecs")
	}
	if rs.Snapshot().Has(Suspended) {
		t.Fatal("runstate.Suspended bit still set after Guard resumed")
	}
}

func TestGuard_PollOnce_NilRsUsesLocalSuspendedFallback(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	unloadCalls := 0
	g := NewGuard(nil, GuardConfig{PSIFullThreshold: 10, PSISustainSecs: 30}, func() { unloadCalls++ })
	g.now = clock.now
	g.memInfo = fakeMemInfo(1000*mib, 500*mib)
	g.psi = func(string) (sysinfo.Pressure, error) { return sysinfo.Pressure{FullAvg10: 50}, nil }

	g.pollOnce()
	clock.advance(31 * time.Second)
	g.pollOnce()
	if !g.suspended() {
		t.Fatal("nil-rs guard never suspended locally")
	}
	if unloadCalls != 1 {
		t.Fatalf("unloadCalls = %d, want 1", unloadCalls)
	}

	// A second tick still above threshold must not call unload again --
	// the local fallback flag must prevent re-triggering just like the
	// rs.Snapshot() check does when rs is wired.
	clock.advance(5 * time.Second)
	g.pollOnce()
	if unloadCalls != 1 {
		t.Fatalf("unloadCalls = %d after a second above-threshold tick, want still 1", unloadCalls)
	}
}

func TestGuard_Run_ReturnsImmediatelyWhenPSIUnavailable(t *testing.T) {
	g := newTestGuard(GuardConfig{}, nil)
	g.psi = func(string) (sysinfo.Pressure, error) { return sysinfo.Pressure{}, errors.New("no PSI") }

	done := make(chan struct{})
	go func() {
		g.Run(context.Background()) // must return on its own, not wait for ctx
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return promptly when PSI is unavailable (AC-018-6)")
	}
}

func TestGuard_Run_StopsOnContextCancel(t *testing.T) {
	g := newTestGuard(GuardConfig{}, nil)
	g.psi = func(string) (sysinfo.Pressure, error) { return sysinfo.Pressure{}, nil }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		g.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}
