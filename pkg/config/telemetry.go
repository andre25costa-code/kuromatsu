package config

import (
	"path/filepath"
	"strings"
)

// TelemetryConfig is the top-level "telemetry" block (ADR-017/FR-019): the
// per-turn SQLite recorder + /stats. Disabled by default: with
// Enabled=false, no turns.db file is ever created (AC-019-6) -- the zero
// value is a strict no-op.
type TelemetryConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// DBPath overrides the turns.db path. Empty means
	// "$KUROMATSU_HOME/telemetry/turns.db" (EffectiveDBPath's default).
	DBPath string `json:"db_path,omitempty"`
	// RetentionDays is how long Prune keeps rows for (AC-019-5). Default 30.
	RetentionDays int `json:"retention_days,omitempty"`
}

const defaultTelemetryRetentionDays = 30

// EffectiveDBPath returns the turns.db path, or "" when telemetry is
// disabled -- callers use that to skip opening the store entirely
// (AC-019-6), the same pattern as RunstateConfig.EffectiveStateFile.
func (c TelemetryConfig) EffectiveDBPath(home string) string {
	if !c.Enabled {
		return ""
	}
	if path := strings.TrimSpace(c.DBPath); path != "" {
		return path
	}
	return filepath.Join(home, "telemetry", "turns.db")
}

func (c TelemetryConfig) EffectiveRetentionDays() int {
	if c.RetentionDays > 0 {
		return c.RetentionDays
	}
	return defaultTelemetryRetentionDays
}
