package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGuardsConfig(t *testing.T, defaults string) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	raw := fmt.Sprintf(`{"version": 3, "agents": {"defaults": {%s}}}`, defaults)
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(configPath): %v", err)
	}
	return configPath
}

func TestLoadConfig_MaxRepeatedToolCallsMinimum(t *testing.T) {
	const wantErr = "agents.defaults.max_repeated_tool_calls must be 0 (off) or >= 2"

	for _, bad := range []int{1, -1} {
		path := writeGuardsConfig(t, fmt.Sprintf(`"max_repeated_tool_calls": %d`, bad))
		_, err := LoadConfig(path)
		if err == nil {
			t.Fatalf("LoadConfig(max_repeated_tool_calls=%d) succeeded, want error", bad)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("LoadConfig(max_repeated_tool_calls=%d) error = %q, want it to contain %q", bad, err, wantErr)
		}
	}

	for _, good := range []int{0, 2} {
		path := writeGuardsConfig(t, fmt.Sprintf(`"max_repeated_tool_calls": %d`, good))
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig(max_repeated_tool_calls=%d) error: %v", good, err)
		}
		if got := cfg.Agents.Defaults.MaxRepeatedToolCalls; got != good {
			t.Fatalf("MaxRepeatedToolCalls = %d, want %d", got, good)
		}
	}
}

func TestDefaultConfig_AgentLoopGuards(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.Agents.Defaults.MaxRepeatedToolCalls; got != 3 {
		t.Fatalf("DefaultConfig MaxRepeatedToolCalls = %d, want 3", got)
	}
	if got := cfg.Agents.Defaults.UserTurnMaxMinutes; got != 30 {
		t.Fatalf("DefaultConfig UserTurnMaxMinutes = %d, want 30", got)
	}
	if err := cfg.ValidateAgentLoopGuards(); err != nil {
		t.Fatalf("DefaultConfig fails ValidateAgentLoopGuards: %v", err)
	}
}

func TestLoadConfig_UserTurnMaxMinutesFromEnv(t *testing.T) {
	t.Setenv("KUROMATSU_AGENTS_DEFAULTS_USER_TURN_MAX_MINUTES", "5")
	path := writeGuardsConfig(t, `"user_turn_max_minutes": 30`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if got := cfg.Agents.Defaults.UserTurnMaxMinutes; got != 5 {
		t.Fatalf("UserTurnMaxMinutes = %d, want 5 from the environment", got)
	}
}
