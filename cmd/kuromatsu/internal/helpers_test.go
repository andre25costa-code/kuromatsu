package internal

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

func TestGetConfigPath(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := filepath.Join("/tmp/home", ".kuromatsu", "config.json")

	assert.Equal(t, want, got)
}

func TestGetConfigPath_WithKUROMATSU_HOME(t *testing.T) {
	t.Setenv(config.EnvHome, "/custom/kuromatsu")
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := filepath.Join("/custom/kuromatsu", "config.json")

	assert.Equal(t, want, got)
}

func TestGetConfigPath_WithKUROMATSU_CONFIG(t *testing.T) {
	t.Setenv("KUROMATSU_CONFIG", "/custom/config.json")
	t.Setenv(config.EnvHome, "/custom/kuromatsu")
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := "/custom/config.json"

	assert.Equal(t, want, got)
}

func TestGetConfigPath_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific HOME behavior varies; run on windows")
	}

	testUserProfilePath := `C:\Users\Test`
	t.Setenv("USERPROFILE", testUserProfilePath)

	got := GetConfigPath()
	want := filepath.Join(testUserProfilePath, ".kuromatsu", "config.json")

	require.True(t, strings.EqualFold(got, want), "GetConfigPath() = %q, want %q", got, want)
}

func TestLoadConfigForEdit_AppliesGatewayLogLevel(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, configPath)

	seed := config.DefaultConfig()
	seed.Gateway.LogLevel = "debug"
	require.NoError(t, config.SaveConfig(configPath, seed))

	previous := logger.GetLevel()
	t.Cleanup(func() { logger.SetLevel(previous) })
	logger.SetLevel(logger.ERROR)

	cfg, err := LoadConfigForEdit()
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Gateway.LogLevel)
	assert.Equal(t, logger.DEBUG, logger.GetLevel())
}
