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
