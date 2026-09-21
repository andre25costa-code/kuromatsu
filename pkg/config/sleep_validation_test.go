package config

import "testing"

func TestSleepRejectsNativeAliasesAndLegacyModelReferences(t *testing.T) {
	for _, provider := range []string{"native", "bonsai", "kuro", "NATIVE"} {
		for _, explicit := range []bool{true, false} {
			mc := &ModelConfig{ModelName: "custom", Provider: provider, Model: "model"}
			if !explicit {
				mc.Provider = ""
				mc.Model = provider + "/model"
			}
			if (SleepConfig{UnconsciousModel: "custom"}).HasValidExternalModel(SecureModelList{mc}) {
				t.Fatalf("accepted native reference: %+v", mc)
			}
		}
	}
}

func TestSleepWindowRejectsOutOfRangeTimes(t *testing.T) {
	for _, window := range []string{"99:99-99:99", "24:00-05:00", "03:60-05:00"} {
		cfg := &Config{Sleep: SleepConfig{Enabled: true, Window: window}}
		if cfg.ValidateSleep() == nil {
			t.Fatalf("accepted %q", window)
		}
	}
}
