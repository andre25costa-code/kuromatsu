// Package hwprofile maps detected hardware to one of four resource
// profiles (Ultra-Light, Light, Balanced, Performance) and writes a
// profile's values into a config as ordinary explicit settings.
//
// There is no "profile" config field: a profile is only a named set of
// existing keys, applied once (at onboard or on request). Match reports
// which profile, if any, a config currently corresponds to.
package hwprofile

import (
	"fmt"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	providercommon "github.com/andre25costa-code/kuromatsu/pkg/providers/common"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/localllm"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// Name identifies a profile.
type Name string

const (
	UltraLight  Name = "Ultra-Light"
	Light       Name = "Light"
	Balanced    Name = "Balanced"
	Performance Name = "Performance"
	// Custom is what Match returns when no profile matches the config.
	Custom Name = "custom"
)

// All lists the profiles from smallest to largest.
var All = []Name{UltraLight, Light, Balanced, Performance}

// UnknownRAMNote is shown when the RAM could not be detected and Select
// fell back to Light.
const UnknownRAMNote = "RAM not detected; using Light"

// Budget bands, in MiB, evaluated in order by Select.
const (
	ultraLightMaxMiB = 768
	lightMaxMiB      = 2048
	balancedMaxMiB   = 8192
	performanceCPUs  = 4
)

// Built-in runtime defaults a zero/absent config value stands for. They
// mirror localllm.Options.WithDefaults (n_ctx), the sub-turn defaults in
// the agent package and the heartbeat service default; the Light profile
// equals all of them.
const (
	defaultNCtx                = 2048
	defaultSubDepth            = 3
	defaultSubConcurrent       = 5
	defaultSubConcurrentGlobal = 8
	defaultHeartbeatMinutes    = 30
	minHeartbeatMinutes        = 5
)

// nativeBuilt is a seam over localllm.Built for tests.
var nativeBuilt = localllm.Built

// Knobs are the values a profile writes.
type Knobs struct {
	NCtx           int // native model extra_body.n_ctx
	NThreads       int // native model extra_body.n_threads
	MinAvailableMB int // memguard.min_available_mb
	MarginMB       int // memguard.margin_mb
	SubDepth       int // agents.defaults.subturn.max_depth
	SubConc        int // agents.defaults.subturn.max_concurrent
	SubGlobal      int // agents.defaults.subturn.max_concurrent_global
	HeartbeatMin   int // heartbeat.interval (minutes)
}

// ParseName resolves a profile name case-insensitively ("ultra-light",
// "Light", ...). ok is false for anything else, including "custom".
func ParseName(s string) (Name, bool) {
	for _, n := range All {
		if strings.EqualFold(s, string(n)) {
			return n, true
		}
	}
	return "", false
}

// Select picks the profile for hw from its RAM budget (the smaller of the
// total RAM and the cgroup limit) and CPU count. Unknown RAM selects Light.
func Select(hw sysinfo.Hardware) Name {
	budget := hw.Budget()
	if budget == 0 {
		return Light
	}
	mib := budget >> 20
	switch {
	case mib < ultraLightMaxMiB:
		return UltraLight
	case mib < lightMaxMiB:
		return Light
	case mib < balancedMaxMiB || hw.CPUs < performanceCPUs:
		return Balanced
	default:
		return Performance
	}
}

// KnobsFor returns profile n's values on hw (thread counts depend on the
// CPU count). An unknown name returns the zero Knobs.
func KnobsFor(n Name, hw sysinfo.Hardware) Knobs {
	cpus := max(hw.CPUs, 1)
	switch n {
	case UltraLight:
		return Knobs{1024, min(cpus, 2), 128, 64, 2, 2, 2, 60}
	case Light:
		return Knobs{2048, min(cpus, 4), 256, 96, 3, 5, 8, 30}
	case Balanced:
		return Knobs{4096, min(cpus, 4), 512, 96, 3, 5, 8, 30}
	case Performance:
		return Knobs{8192, min(cpus, 8), 1024, 128, 3, 8, 16, 15}
	}
	return Knobs{}
}

// defaultThreads is the native engine's own n_threads default on hw.
func defaultThreads(hw sysinfo.Hardware) int {
	return min(max(hw.CPUs, 1), 4)
}

// Apply writes profile n's values into cfg and returns warnings for the
// user. It touches only the profile keys: never memguard.enabled,
// heartbeat.enabled, models (other than the native entry's extra_body),
// channels or permissions, and it never creates the native entry.
//
// Two keys are written only when they differ from the built-in default:
// memguard limits equal to the default are left unset (0), so the Light
// profile leaves a default config's JSON unchanged, and n_threads is
// written only when it differs from the engine's own default for this
// machine (otherwise removed), so a config does not pin the CPU count of
// the machine it was created on.
func Apply(cfg *config.Config, n Name, hw sysinfo.Hardware) (warnings []string) {
	if _, ok := ParseName(string(n)); !ok || cfg == nil {
		return []string{fmt.Sprintf("unknown profile %q; nothing changed", n)}
	}
	k := KnobsFor(n, hw)

	if entry := nativeEntry(cfg); entry != nil {
		if entry.ExtraBody == nil {
			entry.ExtraBody = map[string]any{}
		}
		entry.ExtraBody["n_ctx"] = k.NCtx
		if k.NThreads != defaultThreads(hw) {
			entry.ExtraBody["n_threads"] = k.NThreads
		} else {
			delete(entry.ExtraBody, "n_threads")
		}
	}

	defaults := config.MemguardConfig{}
	cfg.Memguard.MinAvailableMB = unlessDefault(k.MinAvailableMB, defaults.EffectiveMinAvailableMB())
	cfg.Memguard.MarginMB = unlessDefault(k.MarginMB, defaults.EffectiveMarginMB())

	cfg.Agents.Defaults.SubTurn.MaxDepth = k.SubDepth
	cfg.Agents.Defaults.SubTurn.MaxConcurrent = k.SubConc
	cfg.Agents.Defaults.SubTurn.MaxConcurrentGlobal = k.SubGlobal

	cfg.Heartbeat.Interval = k.HeartbeatMin

	if nativeBuilt() && !hw.NativeCPUOK {
		warnings = append(warnings, fmt.Sprintf(
			"this native build needs %s, which this CPU lacks; the native model stays disabled",
			strings.Join(hw.NativeCPUMissing, "+")))
	}
	return warnings
}

// Match returns the profile whose values on hw all equal cfg's effective
// values, or Custom. A zero or absent value counts as its built-in default.
// The native keys are compared only when the native entry exists.
func Match(cfg *config.Config, hw sysinfo.Hardware) Name {
	if cfg == nil {
		return Custom
	}
	cur := effectiveKnobs(cfg, hw)
	entry := nativeEntry(cfg)
	for _, n := range All {
		want := KnobsFor(n, hw)
		if entry == nil {
			want.NCtx, want.NThreads = cur.NCtx, cur.NThreads
		}
		if want == cur {
			return n
		}
	}
	return Custom
}

func effectiveKnobs(cfg *config.Config, hw sysinfo.Hardware) Knobs {
	k := Knobs{
		NCtx:           defaultNCtx,
		NThreads:       defaultThreads(hw),
		MinAvailableMB: cfg.Memguard.EffectiveMinAvailableMB(),
		MarginMB:       cfg.Memguard.EffectiveMarginMB(),
		SubDepth:       orDefault(cfg.Agents.Defaults.SubTurn.MaxDepth, defaultSubDepth),
		SubConc:        orDefault(cfg.Agents.Defaults.SubTurn.MaxConcurrent, defaultSubConcurrent),
		SubGlobal:      orDefault(cfg.Agents.Defaults.SubTurn.MaxConcurrentGlobal, defaultSubConcurrentGlobal),
		HeartbeatMin:   effectiveHeartbeat(cfg.Heartbeat.Interval),
	}
	if entry := nativeEntry(cfg); entry != nil && entry.ExtraBody != nil {
		if v, ok := providercommon.AsInt(entry.ExtraBody["n_ctx"]); ok && v > 0 {
			k.NCtx = v
		}
		if v, ok := providercommon.AsInt(entry.ExtraBody["n_threads"]); ok && v > 0 {
			k.NThreads = v
		}
	}
	return k
}

func nativeEntry(cfg *config.Config) *config.ModelConfig {
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == config.NativeModelName {
			return m
		}
	}
	return nil
}

// effectiveHeartbeat mirrors the heartbeat service: 0 means the default,
// anything else below the minimum is raised to it.
func effectiveHeartbeat(minutes int) int {
	switch {
	case minutes == 0:
		return defaultHeartbeatMinutes
	case minutes < minHeartbeatMinutes:
		return minHeartbeatMinutes
	}
	return minutes
}

func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// unlessDefault returns 0 (unset, meaning "use the default") when v equals
// the built-in default, else v.
func unlessDefault(v, def int) int {
	if v == def {
		return 0
	}
	return v
}
