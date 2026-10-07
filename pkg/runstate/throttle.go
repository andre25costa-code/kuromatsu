package runstate

import (
	"context"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// Throttled thresholds/sustain window (ADR-016 point 2, AC-017-6): fixed,
// not configurable -- unlike memguard's PSI thresholds, there is no
// separate "throttle" config block, since the bit is purely informative
// (never gates a transition) and the numbers come straight from the S06/R7
// e2-micro burst-credit analysis, not from anything an operator would
// reasonably want to tune per deployment.
const (
	throttleOnPercent  = 50.0
	throttleOffPercent = 25.0
	throttleSustain    = 60 * time.Second
	throttlePollPeriod = 5 * time.Second
)

// RunThrottleWatchdog polls cpuStat every 5s, computing the CPU steal%
// delta since the previous sample (sysinfo.CPUStat.StealPercent), and
// toggles the informative Throttled bit (AC-017-6): sustained >50% for
// >=60s turns it on, sustained <25% for >=60s turns it off. Each
// direction's sustain timer resets on any tick that doesn't qualify for
// that direction (including a tick that falls in the 25-50% dead zone) --
// stricter than "eventually accumulates 60s total", closer to "60
// continuous seconds", which is how AC-017-6's "sustentado" reads plainly.
//
// Runs until ctx is done. Started only when runstate.enabled=true
// (gateway.go's installRunstateIntegration, C2) -- with the feature off
// this is never called, so no goroutine/polling exists (AC-017-7). A nil
// Engine, or a platform where the very first cpuStat call errors (no
// /proc/stat, e.g. Windows), returns immediately without starting a
// polling loop -- the same graceful-degradation contract
// memguard.Guard.Run follows for PSI.
func RunThrottleWatchdog(ctx context.Context, e *Engine, cpuStat func() (sysinfo.CPUStat, error)) {
	if e == nil {
		return
	}
	prev, err := cpuStat()
	if err != nil {
		logger.WarnCF("runstate", "CPU stat unavailable, throttle watchdog disabled", map[string]any{
			"error": err.Error(),
		})
		return
	}

	ticker := time.NewTicker(throttlePollPeriod)
	defer ticker.Stop()

	var aboveSince, belowSince time.Time
	on := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur, err := cpuStat()
			if err != nil {
				continue
			}
			stealPct := cur.StealPercent(prev)
			prev = cur
			on = throttlePollOnce(e, stealPct, time.Now(), on, &aboveSince, &belowSince)
		}
	}
}

// throttlePollOnce is RunThrottleWatchdog's per-tick body, split out (and
// given an explicit now, rather than calling time.Now() itself) so
// throttle_test.go can drive the 60s sustain windows deterministically
// with a fake clock instead of racing a real ticker and /proc/stat or
// sleeping for a full minute per assertion. Returns the new "currently on"
// state.
func throttlePollOnce(e *Engine, stealPct float64, now time.Time, on bool, aboveSince, belowSince *time.Time) bool {
	if stealPct > throttleOnPercent {
		if aboveSince.IsZero() {
			*aboveSince = now
		}
		if !on && now.Sub(*aboveSince) >= throttleSustain {
			e.Inc(Throttled)
			on = true
			logger.InfoCF("runstate", "sustained CPU steal above threshold: Throttled on", map[string]any{
				"steal_pct": stealPct,
			})
		}
	} else {
		*aboveSince = time.Time{}
	}

	if stealPct < throttleOffPercent {
		if belowSince.IsZero() {
			*belowSince = now
		}
		if on && now.Sub(*belowSince) >= throttleSustain {
			e.Dec(Throttled)
			on = false
			logger.InfoCF("runstate", "CPU steal recovered below threshold: Throttled off", map[string]any{
				"steal_pct": stealPct,
			})
		}
	} else {
		*belowSince = time.Time{}
	}

	return on
}
