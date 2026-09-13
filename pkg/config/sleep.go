package config

import (
	"fmt"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// SleepConfig is the top-level "sleep" block (FR-010, ADR-006/018, Trilho C
// C5/E9 parte 2): the modo dormir nightly memory-consolidation pass.
// Mirrors pkg/sleep.Config field-for-field (that package deliberately
// doesn't import this one -- see its doc comment); sleep_bridge.go is the
// seam that copies one into the other. Disabled by default (BR-005): with
// Enabled=false, no goroutine/timer is created (AC-010-1).
type SleepConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// Window is "HH:MM-HH:MM" (sleep.ParseWindow). Empty defaults to
	// "03:00-05:00" (EffectiveWindow).
	Window string `json:"window,omitempty"`
	// UnconsciousModel is a model_list ref. ADR-018: required and must not
	// resolve to the native Bonsai provider -- without a valid one, sleep
	// stays off even with Enabled=true (AC-010-7), logged, not a fatal
	// boot error.
	UnconsciousModel string `json:"unconscious_model,omitempty"`
	WeeklyDeep       bool   `json:"weekly_deep,omitempty"`
	// MaxTokensBudget caps tokens spent per run (BR-006). 0 means
	// sleep.Config.WithDefaults' own default (20000).
	MaxTokensBudget int  `json:"max_tokens_budget,omitempty"`
	DryRun          bool `json:"dry_run,omitempty"`
}

const defaultSleepWindow = "03:00-05:00"

// EffectiveWindow returns Window, or the ADR-006 default "03:00-05:00"
// when unset.
func (c SleepConfig) EffectiveWindow() string {
	if w := strings.TrimSpace(c.Window); w != "" {
		return w
	}
	return defaultSleepWindow
}

// ValidateSleep runs at LoadConfig time (mirrors ValidateFocus/
// ValidateReflexes). It only returns an error for a structurally invalid
// config (a Window that doesn't parse); a missing/native
// unconscious_model is deliberately NOT an error here (ADR-018 point 2:
// "não é erro fatal de boot") -- it is instead logged as a warning, and
// sleep_bridge.go (C5) independently checks HasValidExternalModel before
// ever starting the scheduler, producing the exact log line AC-010-7
// specifies.
func (c *Config) ValidateSleep() error {
	if c == nil {
		return nil
	}
	if c.Sleep.Enabled {
		if err := parseHHMMWindow(c.Sleep.EffectiveWindow()); err != nil {
			return fmt.Errorf("sleep.window: %w", err)
		}
		if !c.Sleep.HasValidExternalModel(c.ModelList) {
			logger.WarnC("sleep", "modo dormir requer um modelo externo em sleep.unconscious_model")
		}
	}
	// AC-010-9: same trava, applied to the evolution cold path. Also
	// logged, never a fatal LoadConfig error -- pkg/evolution keeps
	// running turn-case collection either way; only the LLM-driven cold
	// path (pattern clustering/draft generation) is gated, and that gate
	// lives in evolution_bridge.go's dreamGate/model resolution, not here.
	if c.Evolution.Enabled && c.Evolution.RunsColdPathAutomatically() {
		if !c.Evolution.HasValidExternalModel(c.ModelList) {
			logger.WarnC("evolution", "evolução requer um modelo externo em evolution.model")
		}
	}
	return nil
}

// HasValidExternalModel reports whether UnconsciousModel names a model_list
// entry that exists and does not resolve to the native Bonsai provider
// (ADR-018 point 1). False for an empty UnconsciousModel too.
func (c SleepConfig) HasValidExternalModel(models SecureModelList) bool {
	return hasValidExternalModel(c.UnconsciousModel, models)
}

// HasValidExternalModel is the AC-010-9 equivalent of SleepConfig's method,
// for evolution.model: false when empty, when the name isn't found, or when
// it resolves to the native provider.
func (c EvolutionConfig) HasValidExternalModel(models SecureModelList) bool {
	return hasValidExternalModel(c.Model, models)
}

// hasValidExternalModel is the shared ADR-018 check behind both
// SleepConfig and EvolutionConfig: name must be set, must resolve to a real
// model_list entry, and that entry must not be the native provider (either
// by explicit provider: native, or by being the seeded bonsai-local entry
// itself -- Provider can be left blank on custom entries and inferred
// later from Model, so the name check covers that edge case too).
func hasValidExternalModel(name string, models SecureModelList) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	mc := findModelConfigByName(models, name)
	if mc == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(mc.Provider), "native") {
		return false
	}
	if mc.ModelName == nativeModelName {
		return false
	}
	return true
}

// parseHHMMWindow validates "HH:MM-HH:MM" without importing pkg/sleep
// (which would create config -> sleep -> ... -> config-shaped risk; sleep
// stays config-agnostic per its own doc comment). Kept intentionally
// minimal: sleep_bridge.go re-parses with sleep.ParseWindow, the actual
// implementation this mirrors.
func parseHHMMWindow(s string) error {
	start, end, ok := strings.Cut(s, "-")
	if !ok {
		return fmt.Errorf("invalid window %q, want \"HH:MM-HH:MM\"", s)
	}
	for _, part := range []string{start, end} {
		h, m, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return fmt.Errorf("invalid time %q in window %q, want \"HH:MM\"", part, s)
		}
		if !isDigits(h) || !isDigits(m) {
			return fmt.Errorf("invalid time %q in window %q, want \"HH:MM\"", part, s)
		}
	}
	return nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
