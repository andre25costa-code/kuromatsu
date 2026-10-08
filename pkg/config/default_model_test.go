package config

import (
	"os"
	"path/filepath"
	"testing"
)

// N12/M01 (audit round 2): after onboard, model_name is empty. A user who
// adds one API key still got `model "" not found in model_list`. When exactly
// one enabled model has a key and no default is set, that model is the
// default -- the same rule the native model already follows.
func TestLoadConfig_SoleKeyedModelBecomesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"version":3,"agents":{"defaults":{"model_name":""}},
"model_list":[
 {"model_name":"openrouter-auto","model":"openrouter/auto","api_keys":["sk-test"]},
 {"model_name":"gpt-5.4","model":"openai/gpt-5.4"}
]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Agents.Defaults.ModelName; got != "openrouter-auto" {
		t.Fatalf("model_name = %q, want the only model with a key", got)
	}
}

// The field contract (ModelConfig.Enabled) says an omitted "enabled" is
// inferred from the key at load, but only the V1->V2 migration did it: in a
// current config a key added to a seeded entry left it disabled, hidden from
// `kuromatsu model` and refused by `kuromatsu model <name>`. An explicit
// "enabled": false is the user's choice and stays.
func TestLoadConfig_OmittedEnabledIsInferredFromKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"version":3,"model_list":[
 {"model_name":"keyed","model":"openai/a","api_keys":["sk-a"]},
 {"model_name":"off","model":"openai/b","api_keys":["sk-b"],"enabled":false},
 {"model_name":"keyless","model":"openai/c"}
]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := map[string]bool{"keyed": true, "off": false, "keyless": false}
	for _, m := range cfg.ModelList {
		if w, ok := want[m.ModelName]; ok && m.Enabled != w {
			t.Errorf("%s: enabled = %v, want %v", m.ModelName, m.Enabled, w)
		}
	}
	if got := cfg.Agents.Defaults.ModelName; got != "keyed" {
		t.Fatalf("model_name = %q, want the only enabled model with a key", got)
	}
}

func TestApplySoleKeyedDefault_LeavesAmbiguousOrExplicitChoices(t *testing.T) {
	keyed := func(name string, enabled bool) *ModelConfig {
		return &ModelConfig{
			ModelName: name, Model: "openai/" + name, Enabled: enabled,
			APIKeys: SecureStrings{NewSecureString("sk-" + name)},
		}
	}
	cases := []struct {
		name    string
		current string
		models  []*ModelConfig
		want    string
	}{
		{"two keyed models: ambiguous", "", []*ModelConfig{keyed("a", true), keyed("b", true)}, ""},
		{"explicit default is kept", "b", []*ModelConfig{keyed("a", true)}, "b"},
		{"disabled keyed model is not picked", "", []*ModelConfig{keyed("a", false)}, ""},
		{"no keyed model", "", []*ModelConfig{{ModelName: "a", Model: "openai/a", Enabled: true}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{ModelList: tc.models}
			cfg.Agents.Defaults.ModelName = tc.current
			applySoleKeyedDefault(cfg)
			if got := cfg.Agents.Defaults.ModelName; got != tc.want {
				t.Fatalf("model_name = %q, want %q", got, tc.want)
			}
		})
	}
}
