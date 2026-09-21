package config

// MemguardConfig is the top-level "memguard" block (ADR-017/FR-018): the
// dynamic Go memory limit + PSI watchdog that replaces a static, load-
// disconnected GOMEMLIMIT. Disabled by default everywhere: with
// Enabled=false, GOMEMLIMIT (if configured via the env var) is honored
// unchanged and no PSI goroutine runs (AC-018-6) -- the zero value is a
// strict no-op.
type MemguardConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// GOGC is the GC target percentage applied via debug.SetGCPercent once
	// memguard takes over (ADR-017 point 2). Default 50.
	GOGC int `json:"gogc,omitempty"`
	// MarginMB is the fixed safety margin subtracted from Plan()'s budget,
	// on top of the model/KV/compute reservations. Default 96.
	MarginMB int `json:"margin_mb,omitempty"`
	// ComputeBufferMB is the ggml compute-buffer reservation subtracted
	// from Plan()'s budget. Default 96.
	ComputeBufferMB int `json:"compute_buffer_mb,omitempty"`
	// MinAvailableMB is the floor PreLoad checks MemAvailable against
	// (KV+compute+margin, AC-018-2) before allowing a model load. Default
	// 256.
	MinAvailableMB int `json:"min_available_mb,omitempty"`
	// PSISomeThreshold is the "some" PSI percentage (0-100) above which
	// the watchdog runs GC/FreeOSMemory (AC-018-3). Default 20.
	PSISomeThreshold int `json:"psi_some_threshold,omitempty"`
	// PSIFullThreshold is the "full" PSI percentage (0-100) above which,
	// sustained for PSISustainSecs, the watchdog Suspends and unloads the
	// model (AC-018-4). Default 10.
	PSIFullThreshold int `json:"psi_full_threshold,omitempty"`
	// PSISustainSecs is how long a threshold crossing must persist before
	// the watchdog acts (both the full-triggered suspend and the
	// full<5%-triggered resume use this same window, ADR-017 point 4).
	// Default 30.
	PSISustainSecs int `json:"psi_sustain_secs,omitempty"`
}

const (
	defaultMemguardGOGC             = 50
	defaultMemguardMarginMB         = 96
	defaultMemguardComputeBufferMB  = 96
	defaultMemguardMinAvailableMB   = 256
	defaultMemguardPSISomeThreshold = 20
	defaultMemguardPSIFullThreshold = 10
	defaultMemguardPSISustainSecs   = 30
)

func (c MemguardConfig) EffectiveGOGC() int {
	if c.GOGC > 0 {
		return c.GOGC
	}
	return defaultMemguardGOGC
}

func (c MemguardConfig) EffectiveMarginMB() int {
	if c.MarginMB > 0 {
		return c.MarginMB
	}
	return defaultMemguardMarginMB
}

func (c MemguardConfig) EffectiveComputeBufferMB() int {
	if c.ComputeBufferMB > 0 {
		return c.ComputeBufferMB
	}
	return defaultMemguardComputeBufferMB
}

func (c MemguardConfig) EffectiveMinAvailableMB() int {
	if c.MinAvailableMB > 0 {
		return c.MinAvailableMB
	}
	return defaultMemguardMinAvailableMB
}

func (c MemguardConfig) EffectivePSISomeThreshold() int {
	if c.PSISomeThreshold > 0 {
		return c.PSISomeThreshold
	}
	return defaultMemguardPSISomeThreshold
}

func (c MemguardConfig) EffectivePSIFullThreshold() int {
	if c.PSIFullThreshold > 0 {
		return c.PSIFullThreshold
	}
	return defaultMemguardPSIFullThreshold
}

func (c MemguardConfig) EffectivePSISustainSecs() int {
	if c.PSISustainSecs > 0 {
		return c.PSISustainSecs
	}
	return defaultMemguardPSISustainSecs
}
