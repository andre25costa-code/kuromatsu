package config

import (
	"fmt"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
	"github.com/andre25costa-code/kuromatsu/pkg/sleep"
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

// EffectiveSleep resolves the modo-dormir settings for agentID: its own
// agents.list[].sleep override where set, falling back field-by-field to
// the global c.Sleep (mirrors Config.EffectiveHeartbeat). agents.list
// empty (the implicit "main" agent) never has a matching AgentConfig, so
// this is always exactly c.Sleep -- byte-identical to before per-agent
// sleep existed.
//
// Deliberately does not import pkg/routing to normalize agentID, for the
// same reason EffectiveHeartbeat doesn't: routing already imports
// pkg/config, so that would be a cycle. Callers get agentID already
// normalized, from AgentRegistry.ListAgentIDs()/AgentInstance.ID.
func (c *Config) EffectiveSleep(agentID string) SleepConfig {
	eff := c.Sleep
	for _, ac := range c.Agents.List {
		if !strings.EqualFold(strings.TrimSpace(ac.ID), strings.TrimSpace(agentID)) || ac.Sleep == nil {
			continue
		}
		if ac.Sleep.Enabled != nil {
			eff.Enabled = *ac.Sleep.Enabled
		}
		if w := strings.TrimSpace(ac.Sleep.Window); w != "" {
			eff.Window = w
		}
		if m := strings.TrimSpace(ac.Sleep.UnconsciousModel); m != "" {
			eff.UnconsciousModel = m
		}
		if ac.Sleep.WeeklyDeep != nil {
			eff.WeeklyDeep = *ac.Sleep.WeeklyDeep
		}
		if ac.Sleep.MaxTokensBudget > 0 {
			eff.MaxTokensBudget = ac.Sleep.MaxTokensBudget
		}
		if ac.Sleep.DryRun != nil {
			eff.DryRun = *ac.Sleep.DryRun
		}
		break
	}
	return eff
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
	if err := c.validateSleepConfig("sleep", c.Sleep); err != nil {
		return err
	}
	// Per-agent overrides (agents.list[].sleep, ADR-019) must independently
	// satisfy the same window/model gate as the global block -- an agent's
	// own unconscious_model resolving to native, or its own window being
	// malformed, must not slip past validation just because the global
	// sleep block happens to be fine (or disabled). Only agents that
	// actually set an override are checked here; an agent without one
	// resolves to c.Sleep, already validated above.
	for i := range c.Agents.List {
		ac := &c.Agents.List[i]
		if ac.Sleep == nil {
			continue
		}
		label := fmt.Sprintf("agents.list[%s].sleep", strings.TrimSpace(ac.ID))
		if err := c.validateSleepConfig(label, c.EffectiveSleep(ac.ID)); err != nil {
			return err
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

// validateSleepConfig is the shared gate behind ValidateSleep's global and
// per-agent checks: a structurally invalid Window is a fatal LoadConfig
// error (label names which block, e.g. "sleep" or
// "agents.list[estudos].sleep"); a missing/native unconscious_model is
// deliberately NOT an error here (ADR-018 point 2), only a warning --
// sleep_bridge.go independently checks HasValidExternalModel before
// scheduling anything for that agent.
func (c *Config) validateSleepConfig(label string, sc SleepConfig) error {
	if !sc.Enabled {
		return nil
	}
	if err := parseHHMMWindow(sc.EffectiveWindow()); err != nil {
		return fmt.Errorf("%s.window: %w", label, err)
	}
	if !sc.HasValidExternalModel(c.ModelList) {
		logger.WarnCF("sleep", "modo dormir requer um modelo externo em unconscious_model", map[string]any{
			"scope": label,
		})
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
	if common.IsNativeModel(mc.Provider, mc.Model) {
		return false
	}
	if mc.ModelName == nativeModelName {
		return false
	}
	return true
}

// Configuration and scheduling share the same parser. pkg/sleep does not
// depend on configuration, so this dependency does not form a cycle.
func parseHHMMWindow(s string) error {
	_, err := sleep.ParseWindow(s)
	return err
}
