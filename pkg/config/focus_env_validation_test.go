package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

func focusWithWindow(enabled bool, w FocusWindow) FocusConfig {
	return FocusConfig{Enabled: enabled, Windows: map[string]FocusWindow{"chat": w}}
}

func TestValidateFocus_RejectsInvalidMemoryWorkspaceHistory(t *testing.T) {
	cases := []struct {
		name      string
		window    FocusWindow
		wantField string
		wantValue string
	}{
		{"memory", FocusWindow{Memory: "retreived"}, "focus.windows.chat.memory", `"retreived"`},
		{"workspace", FocusWindow{Workspace: "compcat"}, "focus.windows.chat.workspace", `"compcat"`},
		{"history", FocusWindow{History: "of"}, "focus.windows.chat.history", `"of"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFocusConfig(focusWithWindow(true, tc.window))
			if err == nil {
				t.Fatal("validateFocusConfig() = nil, want an error")
			}
			for _, want := range []string{tc.wantField, tc.wantValue, "want one of"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestValidateFocus_AcceptsModesCaseInsensitive(t *testing.T) {
	w := FocusWindow{Memory: " Core ", Workspace: "Compact", History: "OFF"}
	if err := validateFocusConfig(focusWithWindow(true, w)); err != nil {
		t.Fatalf("validateFocusConfig() = %v, want nil", err)
	}
	for _, mem := range []string{"", "default", "core", "off", "retrieved", "RETRIEVED"} {
		if err := validateFocusConfig(focusWithWindow(true, FocusWindow{Memory: mem})); err != nil {
			t.Fatalf("memory %q rejected: %v", mem, err)
		}
	}
}

func TestValidateFocus_DisabledSkipsModeValidation(t *testing.T) {
	w := FocusWindow{Memory: "lixo", Workspace: "lixo", History: "lixo"}
	if err := validateFocusConfig(focusWithWindow(false, w)); err != nil {
		t.Fatalf("validateFocusConfig() = %v, want nil when disabled", err)
	}
}

func TestLoadConfig_MaxSearchResultsLegacyEnvAlias(t *testing.T) {
	const oldName = "KUROMATSU_MAX_SEARCH_RESULTS"
	const newName = "KUROMATSU_TOOLS_DISCOVERY_MAX_SEARCH_RESULTS"

	load := func(t *testing.T, oldVal, newVal string) (int, string) {
		t.Helper()
		// Register both names with t.Setenv so Cleanup restores the original
		// state (including a value copied by applyRenamedEnvKeys), then
		// simulate absence.
		t.Setenv(oldName, "")
		t.Setenv(newName, "")
		os.Unsetenv(oldName)
		os.Unsetenv(newName)
		if oldVal != "" {
			os.Setenv(oldName, oldVal)
		}
		if newVal != "" {
			os.Setenv(newName, newVal)
		}
		renamedEnvWarned.Delete(oldName)

		logPath := filepath.Join(t.TempDir(), "alias.log")
		if err := logger.EnableFileLogging(logPath); err != nil {
			t.Fatal(err)
		}
		defer logger.DisableFileLogging()

		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"version":3}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		logged, _ := os.ReadFile(logPath)
		return cfg.Tools.MCP.Discovery.MaxSearchResults, string(logged)
	}

	t.Run("only old name", func(t *testing.T) {
		got, logged := load(t, "7", "")
		if got != 7 {
			t.Fatalf("MaxSearchResults = %d, want 7", got)
		}
		if !strings.Contains(logged, "is deprecated") {
			t.Fatalf("no deprecation warning in log:\n%s", logged)
		}
		// A second load (gateway reload) still resolves the value but must
		// not warn again.
		os.Unsetenv(newName)
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"version":3}`), 0o600); err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(t.TempDir(), "again.log")
		if err := logger.EnableFileLogging(logPath); err != nil {
			t.Fatal(err)
		}
		defer logger.DisableFileLogging()
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Tools.MCP.Discovery.MaxSearchResults != 7 {
			t.Fatalf("second load = %d, want 7", cfg.Tools.MCP.Discovery.MaxSearchResults)
		}
		if again, _ := os.ReadFile(logPath); strings.Contains(string(again), "is deprecated") {
			t.Fatalf("deprecation warned twice:\n%s", again)
		}
	})
	t.Run("only new name", func(t *testing.T) {
		got, logged := load(t, "", "9")
		if got != 9 {
			t.Fatalf("MaxSearchResults = %d, want 9", got)
		}
		if strings.Contains(logged, "is deprecated") {
			t.Fatalf("unexpected deprecation warning:\n%s", logged)
		}
	})
	t.Run("both names, new wins", func(t *testing.T) {
		if got, _ := load(t, "7", "9"); got != 9 {
			t.Fatalf("MaxSearchResults = %d, want 9", got)
		}
	})
}

func TestEnvExample_DocumentsRuntimeVars(t *testing.T) {
	data, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, line := range []string{
		"# KUROMATSU_KEY_PASSPHRASE=",
		"# KUROMATSU_SSH_KEY_PATH=",
		"# KUROMATSU_BUILTIN_SKILLS=",
		"# OPENCLAW_HOME=",
		"# KUROMATSU_TOOLS_DISCOVERY_MAX_SEARCH_RESULTS=",
	} {
		if !strings.Contains(text, line) {
			t.Errorf(".env.example is missing %q", line)
		}
	}
}
