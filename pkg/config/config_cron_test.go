package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCronConfig(t *testing.T, cron string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"version": 3, "tools": {"cron": {` + cron + `}}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}
	return path
}

func TestDefaultConfig_CronMaxConsecutiveFailures(t *testing.T) {
	t.Setenv("KUROMATSU_HOME", t.TempDir())

	if got := DefaultConfig().Tools.Cron.MaxConsecutiveFailures; got != 5 {
		t.Fatalf("default MaxConsecutiveFailures = %d, want 5", got)
	}

	// 0 means "never auto-disable" and must not be replaced by the default.
	cfg, err := LoadConfig(writeCronConfig(t, `"max_consecutive_failures": 0`))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Tools.Cron.MaxConsecutiveFailures; got != 0 {
		t.Fatalf("explicit 0 loaded as %d", got)
	}

	t.Setenv("KUROMATSU_TOOLS_CRON_MAX_CONSECUTIVE_FAILURES", "9")
	cfg, err = LoadConfig(writeCronConfig(t, `"max_consecutive_failures": 3`))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Tools.Cron.MaxConsecutiveFailures; got != 9 {
		t.Fatalf("MaxConsecutiveFailures = %d, want 9 from the environment", got)
	}
}
