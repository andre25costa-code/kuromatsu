package runstate

import (
	"context"
	"errors"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// mib is the byte-to-MiB conversion factor used throughout this file's
// MB-denominated GuardConfig fields.
const mib = 1024 * 1024

// ErrOverloaded is what PreLoad returns when there isn't enough
// MemAvailable to safely load a model (AC-018-2). The literal word
// "overloaded" is what pkg/providers/localllm/engine_cgo.go's ensureLoaded
// surfaces to its caller, which pipeline_llm.go's existing error
// classification already maps to FailoverOverloaded -- a transitory retry
// that needed no new logic (S17).
var ErrOverloaded = errors.New("localllm: overloaded")

// resumeFullThreshold is the fixed "full" PSI percentage below which a
// Suspended guard is eligible to Resume (ADR-017 point 4: "full<5% por
// >=30s"). Unlike PSIFullThreshold (the suspend trigger), this is not
// separately configurable -- the ADR ties it to the suspend threshold by a
// fixed gap, not an independent knob.
const resumeFullThreshold = 5.0

// gcCooldown bounds GC()+FreeOSMemory() to at most once per minute
// (AC-018-3), regardless of how many poll ticks see "some" pressure in
// that window.
const gcCooldown = time.Minute

// GuardConfig holds Guard's already-resolved tunables (every field is
// assumed final -- e.g. config.MemguardConfig.EffectiveGOGC(), not the raw
// possibly-zero GOGC). Plain primitives, not config.MemguardConfig itself:
// pkg/runstate does not import pkg/config, keeping this file's dependency
// footprint as small as mode.go/engine.go (only pkg/logger and
// pkg/sysinfo here).
type GuardConfig struct {
	GOGC             int
	MarginMB         int
	ComputeBufferMB  int
	MinAvailableMB   int
	PSISomeThreshold int
	PSIFullThreshold int
	PSISustainSecs   int
}

// Guard is the ADR-017 memory watchdog: Plan/ApplyPlan derive a dynamic Go
// memory limit from the cgroup/host budget and the model's own
// reservations; PreLoad gates a model load against low MemAvailable
// (AC-018-2); Run polls PSI and GCs/Suspends/Resumes under pressure
// (AC-018-3/4). Every reader is an injectable field (memInfo/
// cgroupMemory/psi/now) defaulting to the real sysinfo/time functions via
// NewGuard, so tests can inject fakes without touching /proc (memguard_test.go).
type Guard struct {
	cfg    GuardConfig
	rs     *Engine
	unload func()
	now    func() time.Time

	memInfo      func() (total, available uint64, err error)
	cgroupMemory func() (used, limit uint64, ok bool)
	psi          func(resource string) (sysinfo.Pressure, error)

	mu               sync.Mutex
	lastGC           time.Time
	fullSince        time.Time // zero when not currently above PSIFullThreshold
	underSince       time.Time // zero when not currently below resumeFullThreshold
	suspendedLocally bool      // fallback "are we suspended" when rs is nil
}

// NewGuard constructs a Guard. rs may be nil (memguard can run with
// runstate.enabled=false -- it still GCs/unloads under pressure, it just
// has no Suspended bit to publish; see suspended()/suspend()/resume()).
// unload is called (in addition to rs.Suspend(), when rs is non-nil) under
// sustained severe pressure -- production wiring passes localllm.UnloadAll.
// nil unload is a safe no-op.
func NewGuard(rs *Engine, cfg GuardConfig, unload func()) *Guard {
	if unload == nil {
		unload = func() {}
	}
	return &Guard{
		cfg:          cfg,
		rs:           rs,
		unload:       unload,
		now:          time.Now,
		memInfo:      sysinfo.MemInfo,
		cgroupMemory: sysinfo.CgroupMemory,
		psi:          sysinfo.PSI,
	}
}

// availableBudget returns min(cgroup limit, MemTotal) in bytes -- 0 when
// MemInfo itself is unavailable (AC-018-6: no Linux/PSI-ish platform).
func (g *Guard) availableBudget() uint64 {
	memTotal, _, err := g.memInfo()
	if err != nil {
		return 0
	}
	if _, limit, ok := g.cgroupMemory(); ok && limit > 0 && limit < memTotal {
		return limit
	}
	return memTotal
}

// Plan computes the Go memory limit (bytes): min(cgroup, MemTotal) -
// modelBytes - kvBytes - ComputeBufferMB - MarginMB, clamped to at least
// 128 MiB (ADR-017 point 2). Returns 0 if availableBudget() is itself 0
// (no MemInfo reader) -- ApplyPlan treats that as "nothing to apply".
func (g *Guard) Plan(modelBytes, kvBytes uint64) uint64 {
	available := g.availableBudget()
	if available == 0 {
		return 0
	}

	const minLimit = 128 * mib
	reserved := modelBytes + kvBytes + uint64(g.cfg.ComputeBufferMB)*mib + uint64(g.cfg.MarginMB)*mib
	if available <= reserved {
		return minLimit
	}
	limit := available - reserved
	if limit < minLimit {
		return minLimit
	}
	return limit
}

// ApplyPlan computes Plan(modelBytes, kvBytes) and applies it via
// debug.SetMemoryLimit plus debug.SetGCPercent(cfg.GOGC) (ADR-017 point 2,
// AC-018-1). A zero Plan() (no MemInfo reader, AC-018-6) is a no-op: the
// process's existing GOMEMLIMIT (env var, if any) is left exactly as
// configured. Meant to be called from engine_cgo.go's postLoad hook.
func (g *Guard) ApplyPlan(modelBytes, kvBytes uint64) uint64 {
	limit := g.Plan(modelBytes, kvBytes)
	if limit == 0 {
		return 0
	}
	debug.SetMemoryLimit(int64(limit))
	debug.SetGCPercent(g.cfg.GOGC)
	logger.InfoCF("memguard", "applied dynamic memory limit", map[string]any{
		"limit_mb": limit / mib,
		"gogc":     g.cfg.GOGC,
	})
	return limit
}

// PreLoad denies a model load when MemAvailable is below
// kvBytes+computeBytes+MinAvailableMB (AC-018-2), returning ErrOverloaded.
// A guard with no working MemInfo reader always allows the load
// (AC-018-6) -- there is nothing to check, so a static GOMEMLIMIT (if
// configured) remains the only guard on that platform. Meant to be called
// from engine_cgo.go's preLoad hook.
func (g *Guard) PreLoad(kvBytes, computeBytes uint64) error {
	_, available, err := g.memInfo()
	if err != nil {
		return nil
	}
	need := kvBytes + computeBytes + uint64(g.cfg.MinAvailableMB)*mib
	if available < need {
		return ErrOverloaded
	}
	return nil
}

// suspended reports whether the guard currently considers itself
// suspended -- rs.Snapshot().Has(Suspended) when rs is wired, otherwise an
// internal fallback flag (so PSI recovery logic still works with
// runstate.enabled=false).
func (g *Guard) suspended() bool {
	if g.rs != nil {
		return g.rs.Snapshot().Has(Suspended)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.suspendedLocally
}

func (g *Guard) suspend() {
	g.mu.Lock()
	g.suspendedLocally = true
	g.mu.Unlock()
	if g.rs != nil {
		g.rs.Suspend()
	}
	g.unload()
}

func (g *Guard) resume() {
	g.mu.Lock()
	g.suspendedLocally = false
	g.mu.Unlock()
	if g.rs != nil {
		g.rs.Resume()
	}
}

// Run polls PSI ("memory") every 5s until ctx is done, applying AC-018-3/4:
//   - some.avg10 > PSISomeThreshold: GC()+FreeOSMemory(), at most once/min.
//   - full.avg10 > PSIFullThreshold sustained >= PSISustainSecs: suspend
//     (Suspend() + unload()) -- before the OOM killer would otherwise act.
//   - full.avg10 < resumeFullThreshold sustained >= PSISustainSecs while
//     suspended: resume.
//
// A platform with no PSI reader (AC-018-6) logs once and returns
// immediately -- no polling loop ever starts.
func (g *Guard) Run(ctx context.Context) {
	if _, err := g.psi("memory"); err != nil {
		logger.WarnCF("memguard", "PSI unavailable, memory guard watchdog disabled", map[string]any{
			"error": err.Error(),
		})
		return
	}

	const pollInterval = 5 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.pollOnce()
		}
	}
}

// pollOnce is Run's per-tick body, split out so memguard_test.go can drive
// it directly against an injected clock instead of waiting on a real
// ticker.
func (g *Guard) pollOnce() {
	pressure, err := g.psi("memory")
	if err != nil {
		return
	}
	now := g.now()
	sustain := time.Duration(g.cfg.PSISustainSecs) * time.Second

	if pressure.SomeAvg10 > float64(g.cfg.PSISomeThreshold) {
		g.mu.Lock()
		shouldGC := now.Sub(g.lastGC) >= gcCooldown
		if shouldGC {
			g.lastGC = now
		}
		g.mu.Unlock()
		if shouldGC {
			runtime.GC()
			debug.FreeOSMemory()
			logger.InfoC("memguard", "moderate memory pressure: ran GC/FreeOSMemory")
		}
	}

	if pressure.FullAvg10 > float64(g.cfg.PSIFullThreshold) {
		g.mu.Lock()
		if g.fullSince.IsZero() {
			g.fullSince = now
		}
		since := g.fullSince
		g.underSince = time.Time{}
		g.mu.Unlock()

		if now.Sub(since) >= sustain && !g.suspended() {
			g.suspend()
			logger.WarnC("memguard", "sustained severe memory pressure: suspended new entries and unloaded the model")
		}
		return
	}

	g.mu.Lock()
	g.fullSince = time.Time{}
	g.mu.Unlock()

	if pressure.FullAvg10 < resumeFullThreshold && g.suspended() {
		g.mu.Lock()
		if g.underSince.IsZero() {
			g.underSince = now
		}
		since := g.underSince
		g.mu.Unlock()

		if now.Sub(since) >= sustain {
			g.resume()
			logger.InfoC("memguard", "memory pressure recovered: resumed")
		}
	}
}
