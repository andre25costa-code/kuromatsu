package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func saveTimezoneConfig(t *testing.T, tz string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	path := filepath.Join(home, "config.json")
	cfg := DefaultConfig()
	cfg.Timezone = tz
	require.NoError(t, SaveConfig(path, cfg))
	return path
}

func restoreLocal(t *testing.T) {
	t.Helper()
	orig := time.Local
	t.Cleanup(func() {
		time.Local = orig //nolint:gosmopolitan // restore the process timezone after the test
		ResetProcessTimezoneForTest()
	})
}

func TestLoadConfig_TimezoneValidatesOnly(t *testing.T) {
	restoreLocal(t)

	_, err := LoadConfig(saveTimezoneConfig(t, "Mars/Olympus"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `timezone "Mars/Olympus"`)

	before := time.Local
	cfg, err := LoadConfig(saveTimezoneConfig(t, "Asia/Tokyo"))
	require.NoError(t, err)
	assert.Equal(t, "Asia/Tokyo", cfg.Timezone)
	assert.Same(t, before, time.Local, "LoadConfig must not change time.Local")

	// Persisted under the "timezone" key and read back by the edit loader.
	edit, err := LoadConfigForEdit(saveTimezoneConfig(t, "Europe/Lisbon"))
	require.NoError(t, err)
	assert.Equal(t, "Europe/Lisbon", edit.Timezone)
}

func TestApplyProcessTimezone_OnceAndTZPrecedence(t *testing.T) {
	restoreLocal(t)

	tokyo := &Config{Timezone: "Asia/Tokyo"}

	// TZ set: the environment wins and nothing changes.
	ResetProcessTimezoneForTest()
	t.Setenv("TZ", "UTC")
	before := time.Local
	ApplyProcessTimezone(tokyo)
	assert.Same(t, before, time.Local)

	// TZ empty: the configured zone becomes time.Local.
	ResetProcessTimezoneForTest()
	t.Setenv("TZ", "")
	ApplyProcessTimezone(tokyo)
	assert.Equal(t, "Asia/Tokyo", time.Local.String())

	// Once per process: a later call (e.g. after a reload) changes nothing.
	ApplyProcessTimezone(&Config{Timezone: "Europe/Lisbon"})
	assert.Equal(t, "Asia/Tokyo", time.Local.String())
}
