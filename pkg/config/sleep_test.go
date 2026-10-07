package config

import "testing"

func externalModelList() SecureModelList {
	return SecureModelList{
		{ModelName: "sonho-cloud", Provider: "openai", Model: "gpt-5.4"},
		{ModelName: nativeModelName, Provider: "native", Model: "bonsai-test"},
	}
}

func TestSleepConfig_HasValidExternalModel(t *testing.T) {
	models := externalModelList()

	cases := []struct {
		name string
		want bool
	}{
		{"", false},
		{"does-not-exist", false},
		{nativeModelName, false},
		{"sonho-cloud", true},
	}
	for _, tc := range cases {
		c := SleepConfig{UnconsciousModel: tc.name}
		if got := c.HasValidExternalModel(models); got != tc.want {
			t.Errorf("HasValidExternalModel(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEvolutionConfig_HasValidExternalModel(t *testing.T) {
	models := externalModelList()
	if (EvolutionConfig{Model: "sonho-cloud"}).HasValidExternalModel(models) != true {
		t.Fatal("valid external model rejected")
	}
	if (EvolutionConfig{Model: nativeModelName}).HasValidExternalModel(models) != false {
		t.Fatal("native model accepted as external")
	}
	if (EvolutionConfig{}).HasValidExternalModel(models) != false {
		t.Fatal("empty model accepted as external")
	}
}

func TestValidateSleep_DisabledIsAlwaysValid(t *testing.T) {
	cfg := &Config{Sleep: SleepConfig{Enabled: false, Window: "not-a-window-at-all"}}
	if err := cfg.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() with sleep disabled: %v, want nil (window is never checked when off)", err)
	}
}

func TestValidateSleep_RejectsMalformedWindow(t *testing.T) {
	cfg := &Config{Sleep: SleepConfig{Enabled: true, Window: "not-a-window", UnconsciousModel: "sonho-cloud"}, ModelList: externalModelList()}
	if err := cfg.ValidateSleep(); err == nil {
		t.Fatal("ValidateSleep() = nil, want an error for a malformed window")
	}
}

func TestValidateSleep_AcceptsValidWindow(t *testing.T) {
	cfg := &Config{Sleep: SleepConfig{Enabled: true, Window: "03:00-05:00", UnconsciousModel: "sonho-cloud"}, ModelList: externalModelList()}
	if err := cfg.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() = %v, want nil", err)
	}
}

// AC-010-7: enabled + empty/native unconscious_model must NOT fail
// LoadConfig -- it's a warning, sleep just stays off at runtime.
func TestValidateSleep_MissingOrNativeModelIsNotFatal(t *testing.T) {
	cfg := &Config{Sleep: SleepConfig{Enabled: true, UnconsciousModel: ""}, ModelList: externalModelList()}
	if err := cfg.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() with empty unconscious_model = %v, want nil (AC-010-7: not fatal)", err)
	}

	cfg2 := &Config{Sleep: SleepConfig{Enabled: true, UnconsciousModel: nativeModelName}, ModelList: externalModelList()}
	if err := cfg2.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() with native unconscious_model = %v, want nil (AC-010-7: not fatal)", err)
	}
}

// AC-010-9: same non-fatal treatment for evolution.model, and only when
// evolution actually runs a cold path automatically.
func TestValidateSleep_EvolutionMissingModelIsNotFatal(t *testing.T) {
	cfg := &Config{
		Evolution: EvolutionConfig{Enabled: true, Mode: "apply", ColdPathTrigger: "after_turn"},
		ModelList: externalModelList(),
	}
	if err := cfg.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() with evolution enabled and no model = %v, want nil (AC-010-9: not fatal)", err)
	}
}

func TestValidateSleep_EvolutionWithoutColdPathIsIgnored(t *testing.T) {
	// Enabled but never triggers a cold path -- nothing to validate,
	// nothing to warn about.
	cfg := &Config{Evolution: EvolutionConfig{Enabled: true}}
	if err := cfg.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() = %v, want nil", err)
	}
}

func TestSleepConfig_EffectiveWindow(t *testing.T) {
	if got := (SleepConfig{}).EffectiveWindow(); got != "03:00-05:00" {
		t.Fatalf("EffectiveWindow() default = %q, want %q", got, "03:00-05:00")
	}
	if got := (SleepConfig{Window: "22:00-23:30"}).EffectiveWindow(); got != "22:00-23:30" {
		t.Fatalf("EffectiveWindow() explicit = %q, want unchanged", got)
	}
}

// ADR-019 "sono configurável por agente": EffectiveSleep mirrors
// EffectiveHeartbeat's contract field-by-field.

func TestEffectiveSleep_NoAgentsListMatchesGlobal(t *testing.T) {
	cfg := &Config{Sleep: SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud"}}
	if got := cfg.EffectiveSleep("main"); got != cfg.Sleep {
		t.Fatalf("EffectiveSleep(main) = %+v, want unchanged global %+v", got, cfg.Sleep)
	}
}

func TestEffectiveSleep_NoOverrideMatchesGlobal(t *testing.T) {
	cfg := &Config{
		Sleep:  SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud"},
		Agents: AgentsConfig{List: []AgentConfig{{ID: "estudos"}}},
	}
	if got := cfg.EffectiveSleep("estudos"); got != cfg.Sleep {
		t.Fatalf("EffectiveSleep(no override) = %+v, want unchanged global %+v", got, cfg.Sleep)
	}
}

func TestEffectiveSleep_PerAgentOverrideWinsFieldByField(t *testing.T) {
	disabled := false
	cfg := &Config{
		Sleep: SleepConfig{
			Enabled: true, Window: "03:00-05:00", UnconsciousModel: "sonho-cloud",
			WeeklyDeep: true, MaxTokensBudget: 20000, DryRun: false,
		},
		Agents: AgentsConfig{List: []AgentConfig{
			{ID: "sensores", Sleep: &AgentSleepConfig{
				Window:          "01:00-02:00",
				MaxTokensBudget: 5000,
			}},
			{ID: "silenced", Sleep: &AgentSleepConfig{Enabled: &disabled}},
		}},
	}

	got := cfg.EffectiveSleep("sensores")
	if got.Window != "01:00-02:00" {
		t.Fatalf("Window = %q, want the override", got.Window)
	}
	if got.MaxTokensBudget != 5000 {
		t.Fatalf("MaxTokensBudget = %d, want the override", got.MaxTokensBudget)
	}
	// Untouched fields inherit from the global block.
	if !got.Enabled || got.UnconsciousModel != "sonho-cloud" || !got.WeeklyDeep {
		t.Fatalf("untouched fields = %+v, want inherited from global", got)
	}

	if got := cfg.EffectiveSleep("silenced"); got.Enabled {
		t.Fatal("Enabled = true, want the override to turn sleep off for this agent only")
	}
	// The global block itself, and any agent without an override, are
	// unaffected by another agent's override existing.
	if cfg.Sleep.Enabled != true || cfg.Sleep.Window != "03:00-05:00" {
		t.Fatalf("global Sleep mutated by EffectiveSleep: %+v", cfg.Sleep)
	}
}

// ValidateSleep must independently gate a per-agent override the same way
// it gates the global block -- a malformed per-agent window is fatal even
// when the global window is fine (or sleep is globally disabled).

func TestValidateSleep_PerAgentOverrideRejectsMalformedWindow(t *testing.T) {
	cfg := &Config{
		Sleep:     SleepConfig{Enabled: false},
		ModelList: externalModelList(),
		Agents: AgentsConfig{List: []AgentConfig{
			{ID: "estudos", Sleep: &AgentSleepConfig{
				Enabled: boolPtr(true), Window: "not-a-window", UnconsciousModel: "sonho-cloud",
			}},
		}},
	}
	if err := cfg.ValidateSleep(); err == nil {
		t.Fatal("ValidateSleep() = nil, want an error for the agent override's malformed window")
	}
}

// A per-agent unconscious_model resolving to native must not slip past
// validation just because the agent's override doesn't touch every field
// -- EffectiveSleep still merges in whatever model the override sets, and
// ValidateSleep must check the resolved (effective) config, not just the
// literal override struct.
func TestValidateSleep_PerAgentNativeModelIsWarnedNotFatal(t *testing.T) {
	cfg := &Config{
		Sleep:     SleepConfig{Enabled: false},
		ModelList: SecureModelList{{ModelName: nativeModelName, Provider: "native"}},
		Agents: AgentsConfig{List: []AgentConfig{
			{ID: "sensores", Sleep: &AgentSleepConfig{
				Enabled: boolPtr(true), UnconsciousModel: nativeModelName,
			}},
		}},
	}
	if err := cfg.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() with a per-agent native model = %v, want nil (ADR-018: warning, not fatal)", err)
	}
}

func TestValidateSleep_PerAgentOverrideValidWindowPasses(t *testing.T) {
	cfg := &Config{
		Sleep:     SleepConfig{Enabled: false},
		ModelList: externalModelList(),
		Agents: AgentsConfig{List: []AgentConfig{
			{ID: "estudos", Sleep: &AgentSleepConfig{
				Enabled: boolPtr(true), Window: "01:00-02:00", UnconsciousModel: "sonho-cloud",
			}},
		}},
	}
	if err := cfg.ValidateSleep(); err != nil {
		t.Fatalf("ValidateSleep() = %v, want nil for a valid per-agent override", err)
	}
}

func boolPtr(b bool) *bool { return &b }
