package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func newHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	path := filepath.Join(home, "config.json")
	t.Setenv(config.EnvConfig, path)
	t.Setenv(envWebUIToken, "")
	return path
}

func TestUIToken_CreatesOnceAndPrints(t *testing.T) {
	path := newHome(t)
	require.NoError(t, config.SaveConfig(path, config.DefaultConfig()))

	var out bytes.Buffer
	require.NoError(t, printToken(&out, path))
	first := strings.SplitN(out.String(), "\n", 2)[0]
	require.Len(t, first, 64)
	assert.Contains(t, out.String(), "restart the gateway")

	sec, err := os.ReadFile(filepath.Join(filepath.Dir(path), config.SecurityConfigFile))
	require.NoError(t, err)
	assert.Contains(t, string(sec), first)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), first)

	out.Reset()
	require.NoError(t, printToken(&out, path))
	assert.Equal(t, first+"\n", out.String(), "an existing token is printed, not replaced")
}

func TestUIToken_EnvTokenIsNotSaved(t *testing.T) {
	path := newHome(t)
	require.NoError(t, config.SaveConfig(path, config.DefaultConfig()))
	t.Setenv(envWebUIToken, "from-env")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, printToken(&out, path))
	assert.True(t, strings.HasPrefix(out.String(), "from-env\n"))

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	cfg, err := config.LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.WebUI.Token.String())
}

func TestUIOpen_PrintsLoopbackURL(t *testing.T) {
	path := newHome(t)
	cfg := config.DefaultConfig()
	cfg.Gateway.Port = 18791
	require.NoError(t, config.SaveConfig(path, cfg))

	var out bytes.Buffer
	require.NoError(t, printURL(&out, path))
	assert.Equal(t, "http://localhost:18791/ui/\n", out.String())
}
