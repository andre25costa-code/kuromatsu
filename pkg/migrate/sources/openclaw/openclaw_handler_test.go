package openclaw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestNewOpenclawHandler(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)
	require.NotNil(t, handler)
}

func TestNewOpenclawHandlerNoConfig(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.Error(t, err)
}

func TestOpenclawHandlerGetSourceName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)

	assert.Equal(t, "openclaw", handler.GetSourceName())
}

func TestOpenclawHandlerGetSourceHome(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)

	home, err := handler.GetSourceHome()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, home)
}

func TestOpenclawHandlerGetSourceWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)

	workspace, err := handler.GetSourceWorkspace()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmpDir, "workspace"), workspace)
}

func TestOpenclawHandlerGetSourceConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)

	configFile, err := handler.GetSourceConfigFile()
	require.NoError(t, err)
	assert.Equal(t, configPath, configFile)
}

func TestOpenclawHandlerGetSourceConfigFileWithConfigJson(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)

	configFile, err := handler.GetSourceConfigFile()
	require.NoError(t, err)
	assert.Equal(t, configPath, configFile)
}

func TestOpenclawHandlerGetMigrateableFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)

	files := handler.GetMigrateableFiles()
	assert.NotEmpty(t, files)
	assert.Contains(t, files, "AGENTS.md")
	assert.Contains(t, files, "SOUL.md")
	assert.Contains(t, files, "USER.md")
}

func TestOpenclawHandlerGetMigrateableDirs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	handler, err := NewOpenclawHandler(Options{
		SourceHome: tmpDir,
	})
	require.NoError(t, err)

	dirs := handler.GetMigrateableDirs()
	assert.NotEmpty(t, dirs)
	assert.Contains(t, dirs, "memory")
	assert.Contains(t, dirs, "skills")
}

func TestResolveSourceHome(t *testing.T) {
	result, err := resolveSourceHome("/custom/path")
	require.NoError(t, err)
	assert.Equal(t, "/custom/path", result)
}

func TestResolveSourceHomeWithEnvVar(t *testing.T) {
	t.Setenv("OPENCLAW_HOME", "/env/path")

	result, err := resolveSourceHome("")
	require.NoError(t, err)
	assert.Equal(t, "/env/path", result)
}

func TestResolveSourceHomeWithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	result, err := resolveSourceHome("~/openclaw")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "openclaw"), result)
}

func TestFindSourceConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	result, err := findSourceConfig(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, configPath, result)
}

func TestFindSourceConfigWithConfigJson(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configPath, []byte("{}"), 0o644)
	require.NoError(t, err)

	result, err := findSourceConfig(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, configPath, result)
}

func TestFindSourceConfigNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := findSourceConfig(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config file found")
}

func TestMapProvider(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"anthropic", "anthropic"},
		{"claude", "anthropic"},
		{"openai", "openai"},
		{"gpt", "openai"},
		{"groq", "groq"},
		{"ollama", "ollama"},
		{"openrouter", "openrouter"},
		{"deepseek", "deepseek"},
		{"together", "together"},
		{"mistral", "mistral"},
		{"fireworks", "fireworks"},
		{"google", "google"},
		{"gemini", "google"},
		{"xai", "xai"},
		{"grok", "xai"},
		{"cerebras", "cerebras"},
		{"sambanova", "sambanova"},
		{"unknown", "unknown"},
		{"", ""},
	}

	for _, tt := range tests {
		result := mapProvider(tt.input)
		assert.Equal(t, tt.expected, result, "mapProvider(%q)", tt.input)
	}
}

func TestRewriteWorkspacePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"~/.openclaw/workspace", "~/.kuromatsu/workspace"},
		{"/home/user/.openclaw/workspace", "/home/user/.kuromatsu/workspace"},
		{"/path/without/openclaw/change", "/path/without/openclaw/change"},
		{"", ""},
	}

	for _, tt := range tests {
		result := rewriteWorkspacePath(tt.input)
		assert.Equal(t, tt.expected, result, "rewriteWorkspacePath(%q)", tt.input)
	}
}

const telegramOnlySource = `{"channels":{"telegram":{"botToken":"t","allowFrom":["123"]}}}`

func newTestHandler(t *testing.T, srcJSON string) (Operation, string) {
	t.Helper()
	srcHome := t.TempDir()
	srcPath := filepath.Join(srcHome, "openclaw.json")
	require.NoError(t, os.WriteFile(srcPath, []byte(srcJSON), 0o644))
	h, err := NewOpenclawHandler(Options{SourceHome: srcHome})
	require.NoError(t, err)
	return h, srcPath
}

func TestExecuteConfigMigration_EndToEndLoads(t *testing.T) {
	t.Setenv("KUROMATSU_HOME", t.TempDir())
	h, srcPath := newTestHandler(t, telegramOnlySource)
	dst := filepath.Join(t.TempDir(), "out", "config.json")

	require.NoError(t, h.ExecuteConfigMigration(srcPath, dst))

	chans := writtenChannels(t, dst)
	require.Len(t, chans, 1)
	require.Contains(t, chans, "telegram")
	assert.Equal(t, []any{"123"}, chans["telegram"]["allow_from"])

	cfg, err := config.LoadConfig(dst)
	require.NoError(t, err)
	tg := cfg.Channels["telegram"]
	require.NotNil(t, tg)
	assert.Equal(t, []string{"123"}, []string(tg.AllowFrom))
	assert.True(t, tg.Enabled)
}

func TestExecuteConfigMigration_NoChannelsLoads(t *testing.T) {
	t.Setenv("KUROMATSU_HOME", t.TempDir())
	h, srcPath := newTestHandler(t, `{"channels":{}}`)
	dst := filepath.Join(t.TempDir(), "config.json")

	require.NoError(t, h.ExecuteConfigMigration(srcPath, dst))

	assert.Empty(t, writtenChannels(t, dst))
	_, err := config.LoadConfig(dst)
	require.NoError(t, err)
}

func TestValidateMigratedConfig_RejectsUnknownChannelType(t *testing.T) {
	t.Setenv("KUROMATSU_HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Channels = config.ChannelsConfig{"x": &config.Channel{Type: "line"}}

	require.Error(t, config.ValidateForSave(cfg))
	require.NoError(t, config.ValidateForSave(config.DefaultConfig()))
}

func TestExecuteConfigMigration_InvalidResultLeavesDestinationUntouched(t *testing.T) {
	t.Setenv("KUROMATSU_HOME", t.TempDir())
	h, srcPath := newTestHandler(t, telegramOnlySource)

	orig := buildStandardConfig
	t.Cleanup(func() { buildStandardConfig = orig })
	buildStandardConfig = func(c *KuromatsuConfig) *config.Config {
		cfg := c.ToStandardConfig()
		cfg.Channels = config.ChannelsConfig{"x": &config.Channel{Type: "line"}}
		return cfg
	}

	dst := filepath.Join(t.TempDir(), "config.json")
	before := []byte("{\"keep\": \"me\"}\n")
	require.NoError(t, os.WriteFile(dst, before, 0o600))

	err := h.ExecuteConfigMigration(srcPath, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrated config is invalid")

	after, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
	entries, _ := os.ReadDir(filepath.Dir(dst))
	assert.Len(t, entries, 1, "no companion file may be written next to the destination")
}

// writtenChannels returns the channel_list object exactly as it is on disk,
// before LoadConfig fills in defaults for channels the file does not mention.
func writtenChannels(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc struct {
		Channels map[string]map[string]any `json:"channel_list"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc.Channels
}

// Validating the migrated config never repoints the process-wide secret
// resolver: it keeps resolving against the directory it was set to.
func TestExecuteConfigMigration_FailedValidationLeavesSecretResolver(t *testing.T) {
	t.Setenv("KUROMATSU_HOME", t.TempDir())
	h, srcPath := newTestHandler(t, telegramOnlySource)

	orig := buildStandardConfig
	t.Cleanup(func() { buildStandardConfig = orig })
	buildStandardConfig = func(c *KuromatsuConfig) *config.Config {
		cfg := c.ToStandardConfig()
		cfg.Channels = config.ChannelsConfig{"x": &config.Channel{Type: "line"}}
		return cfg
	}

	dstDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, "tok.key"), []byte("s3cret\n"), 0o600))
	// Point the resolver at the destination, as loading its config does.
	_, err := config.LoadConfigForEdit(filepath.Join(dstDir, "config.json"))
	require.NoError(t, err)

	require.Error(t, h.ExecuteConfigMigration(srcPath, filepath.Join(dstDir, "config.json")))

	var s config.SecureString
	require.NoError(t, s.UnmarshalText([]byte("file://tok.key")))
	assert.Equal(t, "s3cret", s.String())
}
