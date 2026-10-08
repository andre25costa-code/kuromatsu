package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
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

// onboard writes an empty .security.yml entry for every model
// (`gpt-5.4:0: {}`). Loading it replaced the model's api_keys with nothing,
// so a key typed into config.json -- what onboard used to tell the user to
// do -- was dropped. An empty entry leaves the config.json key; a key in the
// security file still wins.
func TestLoadConfig_EmptySecurityEntryKeepsConfigKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"version":3,"model_list":[
 {"model_name":"inline","model":"openai/a","api_keys":["sk-inline"]},
 {"model_name":"secured","model":"openai/b","api_keys":["sk-old"]}
]}`
	sec := "model_list:\n  inline:0: {}\n  secured:0:\n    api_keys:\n      - sk-secure\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(securityPath(path), []byte(sec), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := map[string]string{"inline": "sk-inline", "secured": "sk-secure"}
	for _, m := range cfg.ModelList {
		if w, ok := want[m.ModelName]; ok && m.APIKey() != w {
			t.Errorf("%s: api key = %q, want %q", m.ModelName, m.APIKey(), w)
		}
	}
}

// N05 (audit round 2): a version migration saved the config with a defer
// that ran after env.Parse and the runtime defaults, so values that only
// existed in the environment were written into config.json; and it saved
// even when LoadConfig then failed validation. The migrated file holds the
// migration result only, and is written only when loading succeeds.
func TestLoadConfig_MigrationSavesNeitherEnvNorInvalidConfigs(t *testing.T) {
	t.Run("env values are not persisted", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"version":2}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("KUROMATSU_TOOLS_READ_FILE_MAX_READ_FILE_SIZE", "12345")
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Tools.ReadFile.MaxReadFileSize != 12345 {
			t.Fatal("env override not applied at runtime")
		}
		data, _ := os.ReadFile(path)
		var saved struct {
			Version int `json:"version"`
			Tools   struct {
				ReadFile struct {
					MaxReadFileSize int `json:"max_read_file_size"`
				} `json:"read_file"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Version != CurrentVersion {
			t.Fatalf("migrated file version = %d, want %d", saved.Version, CurrentVersion)
		}
		if saved.Tools.ReadFile.MaxReadFileSize == 12345 {
			t.Fatal("KUROMATSU_TOOLS_READ_FILE_MAX_READ_FILE_SIZE was written into config.json")
		}
	})

	t.Run("invalid config is not rewritten", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		body := `{"version":2,"model_list":[
 {"model_name":"bad","model":"/openai/a"}]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatal("a model id starting with / must fail validation")
		}
		if data, _ := os.ReadFile(path); string(data) != body {
			t.Fatalf("config.json rewritten although LoadConfig failed:\n%s", data)
		}
	})
}

// N15 (audit round 2): voice.elevenlabs_api_key was accepted and never read
// -- ElevenLabs transcription takes its key from the model_list entry. The
// strict decoder rejects unknown fields, so the field stays (a config that
// carries it keeps loading) and a warning says where the key belongs.
func TestLoadConfig_WarnsAboutIgnoredElevenLabsKey(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "voice.log")
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatal(err)
	}
	defer logger.DisableFileLogging()

	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"version":3,"voice":{"elevenlabs_api_key":"sk-old","echo_transcription":true}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Voice.EchoTranscription {
		t.Fatal("the rest of the voice section was not loaded")
	}
	if logged, _ := os.ReadFile(logPath); !strings.Contains(string(logged), "elevenlabs_api_key") {
		t.Fatalf("no warning about the ignored key: %s", logged)
	}
}
