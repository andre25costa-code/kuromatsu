package runstate

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

func TestThrottlePollOnce_TurnsOnAfterSustainedHighSteal(t *testing.T) {
	e := New()
	t0 := time.Now()
	var above, below time.Time

	on := throttlePollOnce(e, 60, t0, false, &above, &below)
	if on {
		t.Fatal("turned on before the sustain window elapsed")
	}
	if e.Snapshot().Has(Throttled) {
		t.Fatal("Throttled set before the sustain window elapsed")
	}

	on = throttlePollOnce(e, 60, t0.Add(59*time.Second), on, &above, &below)
	if on {
		t.Fatal("turned on one second before the 60s sustain window elapses")
	}

	on = throttlePollOnce(e, 60, t0.Add(61*time.Second), on, &above, &below)
	if !on {
		t.Fatal("did not turn on after 61s of sustained steal > 50%")
	}
	if !e.Snapshot().Has(Throttled) {
		t.Fatal("Throttled bit not set on the engine after turning on")
	}
}

func TestThrottlePollOnce_ResetsAboveTimerOnADippingTick(t *testing.T) {
	e := New()
	t0 := time.Now()
	var above, below time.Time

	on := throttlePollOnce(e, 60, t0, false, &above, &below) // above = t0

	// A single dead-zone tick resets the above-timer entirely.
	on = throttlePollOnce(e, 30, t0.Add(10*time.Second), on, &above, &below)
	if on {
		t.Fatal("dead-zone dip must not turn Throttled on")
	}

	// First qualifying tick after the dip sets a NEW aboveSince (t0+20s).
	on = throttlePollOnce(e, 60, t0.Add(20*time.Second), on, &above, &below)
	if on {
		t.Fatal("turned on immediately on the first tick after the dip")
	}

	// Only 59s after the post-dip reset point: must still be off.
	on = throttlePollOnce(e, 60, t0.Add(79*time.Second), on, &above, &below)
	if on {
		t.Fatal("turned on before 60s elapsed from the post-dip reset point")
	}

	// 61s after the post-dip reset point: must turn on.
	on = throttlePollOnce(e, 60, t0.Add(81*time.Second), on, &above, &below)
	if !on {
		t.Fatal("never turned on 61s after the post-dip reset point")
	}
}

func TestThrottlePollOnce_TurnsOffAfterSustainedLowSteal(t *testing.T) {
	e := New()
	e.Inc(Throttled) // start "on", as if a prior sustained-high-steal window already fired
	t0 := time.Now()
	var above, below time.Time

	on := throttlePollOnce(e, 10, t0, true, &above, &below)
	if !on {
		t.Fatal("turned off before the sustain window elapsed")
	}

	on = throttlePollOnce(e, 10, t0.Add(59*time.Second), on, &above, &below)
	if !on {
		t.Fatal("turned off one second before the 60s sustain window elapses")
	}

	on = throttlePollOnce(e, 10, t0.Add(61*time.Second), on, &above, &below)
	if on {
		t.Fatal("did not turn off after 61s of sustained steal < 25%")
	}
	if e.Snapshot().Has(Throttled) {
		t.Fatal("Throttled bit still set on the engine after turning off")
	}
}

func TestThrottlePollOnce_DeadZoneNeitherTurnsOnNorOff(t *testing.T) {
	e := New()
	t0 := time.Now()
	var above, below time.Time

	on := throttlePollOnce(e, 35, t0, false, &above, &below)
	on = throttlePollOnce(e, 35, t0.Add(120*time.Second), on, &above, &below)
	if on {
		t.Fatal("dead-zone (25-50%) steal must never turn Throttled on")
	}
	if e.Snapshot().Has(Throttled) {
		t.Fatal("Throttled set from dead-zone steal alone")
	}
}

func TestRunThrottleWatchdog_ReturnsImmediatelyOnNilEngine(t *testing.T) {
	var cpuStatCalled atomic.Bool
	done := make(chan struct{})
	go func() {
		RunThrottleWatchdog(context.Background(), nil, func() (sysinfo.CPUStat, error) {
			cpuStatCalled.Store(true)
			return sysinfo.CPUStat{}, nil
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunThrottleWatchdog(nil engine) did not return promptly")
	}
	if cpuStatCalled.Load() {
		t.Fatal("cpuStat was called despite a nil engine -- the nil check must short-circuit before reading CPU stats")
	}
}

func TestRunThrottleWatchdog_ReturnsImmediatelyWhenCPUStatUnavailable(t *testing.T) {
	e := New()
	done := make(chan struct{})
	go func() {
		RunThrottleWatchdog(context.Background(), e, func() (sysinfo.CPUStat, error) {
			return sysinfo.CPUStat{}, errors.New("no /proc/stat")
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunThrottleWatchdog did not return promptly when cpuStat is unavailable (AC-017-6 platform fallback)")
	}
}

func TestRunThrottleWatchdog_StopsOnContextCancel(t *testing.T) {
	e := New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunThrottleWatchdog(ctx, e, func() (sysinfo.CPUStat, error) {
			return sysinfo.CPUStat{}, nil
		})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunThrottleWatchdog did not stop after context cancellation")
	}
}
