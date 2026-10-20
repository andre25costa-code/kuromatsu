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
	assert.NotContains(t, string(data), `"webui"`, "a default block is left out of config.json")

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

// A default webui block is not written to config.json (a binary older than
// the block rejects unknown keys), while non-default values round-trip.
func TestWebUI_DefaultBlockOmittedNonDefaultKept(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	path := filepath.Join(home, "config.json")
	require.NoError(t, SaveConfig(path, DefaultConfig()))

	edit, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	require.NoError(t, SaveConfig(path, edit))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"webui"`)

	for name, set := range map[string]func(*WebUIConfig){
		"disabled":     func(w *WebUIConfig) { w.Enabled = false },
		"allow_remote": func(w *WebUIConfig) { w.AllowRemote = true },
	} {
		cfg, err := LoadConfigForEdit(path)
		require.NoError(t, err)
		want := cfg.WebUI
		set(&want)
		cfg.WebUI = want
		require.NoError(t, SaveConfig(path, cfg))
		raw, err = os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"webui"`, name)

		back, err := LoadConfigForEdit(path)
		require.NoError(t, err)
		assert.Equal(t, want.Enabled, back.WebUI.Enabled, name)
		assert.Equal(t, want.AllowRemote, back.WebUI.AllowRemote, name)
		rt, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, want.Enabled, rt.WebUI.Enabled, name)

		// Back to defaults for the next case.
		back.WebUI = WebUIConfig{Enabled: true}
		require.NoError(t, SaveConfig(path, back))
	}
}

// The token stays in the security file even when the block is omitted
// from config.json.
func TestWebUI_TokenKeptWhenBlockOmitted(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	path := filepath.Join(home, "config.json")
	cfg := DefaultConfig()
	_, err := cfg.WebUI.EnsureToken()
	require.NoError(t, err)
	token := cfg.WebUI.Token.String()
	require.NoError(t, SaveConfig(path, cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"webui"`)
	back, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.Equal(t, token, back.WebUI.Token.String())
	assert.True(t, back.WebUI.Enabled)
}
