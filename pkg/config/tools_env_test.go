package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// Every tool must be switchable with KUROMATSU_TOOLS_<NAME>_ENABLED. caarlos0/env
// accumulates envPrefix across nesting levels, so a prefix repeated on an
// embedded struct, or a field without an env tag, silently breaks the name.
func TestLoadConfig_ToolEnabledEnvVars(t *testing.T) {
	cases := []struct {
		env     string
		enabled func(*Config) bool
	}{
		{"KUROMATSU_TOOLS_SYSMON_ENABLED", func(c *Config) bool { return c.Tools.Sysmon.Enabled }},
		{"KUROMATSU_TOOLS_READ_FILE_ENABLED", func(c *Config) bool { return c.Tools.ReadFile.Enabled }},
		{"KUROMATSU_TOOLS_WRITE_FILE_ENABLED", func(c *Config) bool { return c.Tools.WriteFile.Enabled }},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			body := `{"version":3,"tools":{"sysmon":{"enabled":false},"read_file":{"enabled":false},"write_file":{"enabled":false}}}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(tc.env, "true")

			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if !tc.enabled(cfg) {
				t.Fatalf("%s=true did not enable the tool", tc.env)
			}
		})
	}
}

func TestLoadConfig_ReadFileEnvVarsOverrideModeAndSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUROMATSU_TOOLS_READ_FILE_MODE", ReadFileModeLines)
	t.Setenv("KUROMATSU_TOOLS_READ_FILE_MAX_READ_FILE_SIZE", "12345")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Tools.ReadFile.Mode != ReadFileModeLines {
		t.Fatalf("Mode = %q, want %q", cfg.Tools.ReadFile.Mode, ReadFileModeLines)
	}
	if cfg.Tools.ReadFile.MaxReadFileSize != 12345 {
		t.Fatalf("MaxReadFileSize = %d, want 12345", cfg.Tools.ReadFile.MaxReadFileSize)
	}
}

// T38: an invalid KUROMATSU_CHANNELS_TELEGRAM_STREAMING_* value was dropped
// in silence. It is reported (the caller logs it, like the other channel env
// overrides on this path) and the configured value is kept.
func TestTelegramStreamingEnvCompat_ReportsInvalidValues(t *testing.T) {
	settings := &TelegramSettings{}
	settings.Streaming.Enabled = true
	settings.Streaming.ThrottleSeconds = 3

	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_STREAMING_ENABLED", "sim")
	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_STREAMING_THROTTLE_SECONDS", "2s")
	err := applyTelegramStreamingEnvCompat(settings)
	if err == nil {
		t.Fatal("invalid values were not reported")
	}
	for _, name := range []string{"STREAMING_ENABLED", "STREAMING_THROTTLE_SECONDS"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name %s: %v", name, err)
		}
	}
	if !settings.Streaming.Enabled || settings.Streaming.ThrottleSeconds != 3 {
		t.Fatalf("invalid values changed the settings: %+v", settings.Streaming)
	}

	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_STREAMING_ENABLED", "false")
	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_STREAMING_THROTTLE_SECONDS", "5")
	if err := applyTelegramStreamingEnvCompat(settings); err != nil {
		t.Fatalf("valid values reported as errors: %v", err)
	}
	if settings.Streaming.Enabled || settings.Streaming.ThrottleSeconds != 5 {
		t.Fatalf("valid values not applied: %+v", settings.Streaming)
	}
}

// T39: migrating a v1 config logged every key-less model at Info (a debug
// leftover) and warned "model_list is not a slice" for a config that simply
// has no model_list, which is valid. Only a model_list of the wrong type is
// worth a warning.
func TestMigrateV1ToV2_LogsOnlyRealProblems(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "migrate.log")
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatal(err)
	}
	defer logger.DisableFileLogging()

	if err := migrateV1ToV2(map[string]any{"version": 1}); err != nil {
		t.Fatal(err)
	}
	if err := migrateV1ToV2(map[string]any{
		"version":    1,
		"model_list": []any{map[string]any{"model_name": "local", "model": "native/bonsai"}},
	}); err != nil {
		t.Fatal(err)
	}
	logged, _ := os.ReadFile(logPath)
	if strings.Contains(string(logged), "model_list is not a slice") {
		t.Fatalf("warned about a missing model_list:\n%s", logged)
	}
	if strings.Contains(string(logged), "model: map[") {
		t.Fatalf("debug line for each model still logged:\n%s", logged)
	}

	if err := migrateV1ToV2(map[string]any{"version": 1, "model_list": map[string]any{"oops": true}}); err != nil {
		t.Fatal(err)
	}
	logged, _ = os.ReadFile(logPath)
	if !strings.Contains(string(logged), "model_list is not a slice") {
		t.Fatalf("a model_list of the wrong type must still warn:\n%s", logged)
	}
}
