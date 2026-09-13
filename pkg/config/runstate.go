package config

import (
	"path/filepath"
	"strings"
)

// RunstateConfig is the top-level "runstate" block (ADR-016/FR-017, S09):
// the single on/off switch for the pkg/runstate state machine's SO
// integration -- state file, sd_notify, and heartbeat preemption. Disabled
// by default everywhere except the demetrius deploy target: with
// Enabled=false, no goroutine/file/sd_notify is created and heartbeat is
// never skipped (AC-017-7) -- the zero value is a strict no-op.
type RunstateConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// StateFile overrides the run/state file path. Empty means
	// "$KUROMATSU_HOME/run/state" (EffectiveStateFile's default).
	StateFile string `json:"state_file,omitempty"`
	// SdNotify gates the sd_notify publisher (STATUS=<names>, SdReady/
	// SdStopping) independently of the run/state file. Defaults to true
	// when Enabled -- most operators who turn runstate on want both.
	SdNotify *bool `json:"sd_notify,omitempty"`
	// SkipHeartbeatWhenBusy gates whether the heartbeat service consults
	// runstate at all (SetShouldSkip). Defaults to true when Enabled.
	SkipHeartbeatWhenBusy *bool `json:"skip_heartbeat_when_busy,omitempty"`
}

// EffectiveStateFile returns the run/state file path to publish to, or ""
// when the file publisher should not run at all (Enabled=false). home is
// $KUROMATSU_HOME (config.GetHome()); callers pass it in explicitly so this
// package -- and pkg/runstate, which only ever receives a plain path --
// never needs to re-derive it.
func (c RunstateConfig) EffectiveStateFile(home string) string {
	if !c.Enabled {
		return ""
	}
	if path := strings.TrimSpace(c.StateFile); path != "" {
		return path
	}
	return filepath.Join(home, "run", "state")
}

// EffectiveSdNotify reports whether the sd_notify publisher should run.
// Always false when Enabled is false.
func (c RunstateConfig) EffectiveSdNotify() bool {
	if !c.Enabled {
		return false
	}
	if c.SdNotify == nil {
		return true
	}
	return *c.SdNotify
}

// EffectiveSkipHeartbeatWhenBusy reports whether the heartbeat service
// should install runstate.SetShouldSkip. Always false when Enabled is
// false.
func (c RunstateConfig) EffectiveSkipHeartbeatWhenBusy() bool {
	if !c.Enabled {
		return false
	}
	if c.SkipHeartbeatWhenBusy == nil {
		return true
	}
	return *c.SkipHeartbeatWhenBusy
}
