package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebUI_DefaultsOnWithoutToken(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	cfg := DefaultConfig()
	assert.True(t, cfg.WebUI.Enabled)
	assert.False(t, cfg.WebUI.AllowRemote)
	assert.Empty(t, cfg.WebUI.Token.String())
}

// A config written before the webui block existed loads with the page on.
func TestWebUI_MissingBlockLoadsEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	path := filepath.Join(home, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":3,"heartbeat":{"enabled":true,"interval":30}}`), 0o600))

	cfg, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.True(t, cfg.WebUI.Enabled)
}

func TestWebUI_TokenSavedOnlyToSecurityFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	path := filepath.Join(home, "config.json")

	cfg := DefaultConfig()
	created, err := cfg.WebUI.EnsureToken()
	require.NoError(t, err)
	require.True(t, created)
	token := cfg.WebUI.Token.String()
	require.Len(t, token, 64)
	require.NoError(t, SaveConfig(path, cfg))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), token)
	assert.Contains(t, string(data), `"webui"`)

	secPath := filepath.Join(home, SecurityConfigFile)
	sec, err := os.ReadFile(secPath)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(sec), token), "token must be in the security file")
	if info, statErr := os.Stat(secPath); statErr == nil && os.PathSeparator == '/' {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	loaded, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.Equal(t, token, loaded.WebUI.Token.String())

	// EnsureToken never rotates an existing token.
	created, err = loaded.WebUI.EnsureToken()
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, token, loaded.WebUI.Token.String())
}

func TestWebUI_EnvTokenCountsButIsNotSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	path := filepath.Join(home, "config.json")
	require.NoError(t, SaveConfig(path, DefaultConfig()))

	t.Setenv("KUROMATSU_WEBUI_TOKEN", "env-webui-token")
	t.Setenv("KUROMATSU_WEBUI_ALLOW_REMOTE", "true")
	rt, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "env-webui-token", rt.WebUI.Token.String())
	assert.True(t, rt.WebUI.AllowRemote)

	edit, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.Empty(t, edit.WebUI.Token.String())
	assert.False(t, edit.WebUI.AllowRemote)
}
