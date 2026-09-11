package internal

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
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
