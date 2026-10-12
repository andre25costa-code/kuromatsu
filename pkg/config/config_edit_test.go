package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const envMaxTokens = "KUROMATSU_AGENTS_DEFAULTS_MAX_TOKENS"

func editTestPaths(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "config.json"), filepath.Join(dir, SecurityConfigFile)
}

func readRaw(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func editSaveCycle(t *testing.T, path string) *Config {
	t.Helper()
	cfg, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	require.NoError(t, SaveConfig(path, cfg))
	return cfg
}

func TestLoadConfigForEdit_IgnoresEnvironment(t *testing.T) {
	path, _ := editTestPaths(t)
	seed := DefaultConfig()
	seed.Agents.Defaults.MaxTokens = 32768
	require.NoError(t, SaveConfig(path, seed))

	t.Setenv(envMaxTokens, "1234")

	editCfg, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.Equal(t, 32768, editCfg.Agents.Defaults.MaxTokens)

	runtimeCfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, 1234, runtimeCfg.Agents.Defaults.MaxTokens)
}

func TestLoadConfigForEdit_SaveDoesNotPersistEnvSecrets(t *testing.T) {
	path, secPath := editTestPaths(t)
	seed := DefaultConfig()
	seed.Channels["telegram"] = &Channel{Enabled: true, Type: "telegram", Settings: RawNode(`{}`)}
	require.NoError(t, SaveConfig(path, seed))

	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_TOKEN", "env-telegram-token")
	t.Setenv("KUROMATSU_SKILLS_REGISTRIES_GITHUB_AUTH_TOKEN", "env-gh-token")

	editSaveCycle(t, path)

	for _, p := range []string{path, secPath} {
		raw := readRaw(t, p)
		assert.NotContains(t, raw, "env-telegram-token", p)
		assert.NotContains(t, raw, "env-gh-token", p)
	}
}

func TestLoadConfigForEdit_KeepsSecurityFileSecrets(t *testing.T) {
	path, secPath := editTestPaths(t)
	seed := DefaultConfig()
	seed.ModelList = append(seed.ModelList, &ModelConfig{
		ModelName: "file-model",
		Model:     "openai/file-model",
		APIKeys:   SimpleSecureStrings("sk-file"),
		Enabled:   true,
	})
	require.NoError(t, SaveConfig(path, seed))
	require.Contains(t, readRaw(t, secPath), "sk-file")

	cfg := editSaveCycle(t, path)

	var found *ModelConfig
	for _, m := range cfg.ModelList {
		if m.ModelName == "file-model" {
			found = m
		}
	}
	require.NotNil(t, found)
	require.Len(t, found.APIKeys, 1)
	assert.Equal(t, "sk-file", found.APIKeys[0].String())
	assert.Contains(t, readRaw(t, secPath), "sk-file")
	assert.NotContains(t, readRaw(t, path), "sk-file")
}

func TestLoadConfigForEdit_ChannelSecretsStayInSecurityFile(t *testing.T) {
	path, secPath := editTestPaths(t)
	seed := DefaultConfig()
	bc := &Channel{Enabled: true, Type: "telegram", Settings: RawNode(`{}`)}
	settings := &TelegramSettings{}
	require.NoError(t, bc.Decode(settings))
	settings.Token = *NewSecureString("123:tg-secret-token")
	settings.UseMarkdownV2 = true
	seed.Channels["telegram"] = bc
	require.NoError(t, SaveConfig(path, seed))
	require.NotContains(t, readRaw(t, path), "123:tg-secret-token", "seeded config.json must not hold the token")
	require.Contains(t, readRaw(t, secPath), "123:tg-secret-token", "seeded security file must hold the token")

	editSaveCycle(t, path)

	rawConfig := readRaw(t, path)
	assert.NotContains(t, rawConfig, "123:tg-secret-token")
	assert.Contains(t, readRaw(t, secPath), "123:tg-secret-token")

	var doc struct {
		ChannelList map[string]struct {
			Settings map[string]any `json:"settings"`
		} `json:"channel_list"`
	}
	require.NoError(t, json.Unmarshal([]byte(rawConfig), &doc))
	assert.Equal(t, true, doc.ChannelList["telegram"].Settings["use_markdown_v2"])
}

// A current-version file cannot carry the legacy "bindings" key: both loaders
// reject it as an unknown field, so an edit command never overwrites it.
func TestLoadConfigForEdit_RejectsLegacyBindingsInCurrentVersion(t *testing.T) {
	path, _ := editTestPaths(t)
	raw := `{
		"version": 3,
		"agents": {
			"defaults": {
				"workspace": "~/.kuromatsu/workspace",
				"model": "glm-4.7"
			},
			"list": [
				{ "id": "main", "default": true },
				{ "id": "support" },
				{ "id": "ops" },
				{ "id": "slack" }
			]
		},
		"bindings": [
			{
				"agent_id": "support",
				"match": {
					"channel": "telegram",
					"peer": { "kind": "group", "id": "-100123" }
				}
			},
			{
				"agent_id": "ops",
				"match": {
					"channel": "discord",
					"guild_id": "guild-1"
				}
			},
			{
				"agent_id": "slack",
				"match": {
					"channel": "slack",
					"account_id": "*"
				}
			}
		]
	}`
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))

	_, editErr := LoadConfigForEdit(path)
	require.Error(t, editErr)
	assert.Contains(t, editErr.Error(), "bindings")

	_, runtimeErr := LoadConfig(path)
	require.Error(t, runtimeErr)
	assert.Equal(t, runtimeErr.Error(), editErr.Error())

	assert.Equal(t, raw, readRaw(t, path), "the file must be left as it was")
}

func TestLoadConfigForEdit_MissingOrTinyFileReturnsDefaults(t *testing.T) {
	want := DefaultConfig()

	t.Run("missing", func(t *testing.T) {
		path, _ := editTestPaths(t)
		cfg, err := LoadConfigForEdit(path)
		require.NoError(t, err)
		assert.Equal(t, want.Agents.Defaults.MaxTokens, cfg.Agents.Defaults.MaxTokens)
		assert.Equal(t, want.Agents.Defaults.Workspace, cfg.Agents.Defaults.Workspace)
		assert.Equal(t, len(want.ModelList), len(cfg.ModelList))
		_, statErr := os.Stat(path)
		assert.True(t, os.IsNotExist(statErr), "loading must not create the file")
	})

	t.Run("tiny", func(t *testing.T) {
		path, _ := editTestPaths(t)
		require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
		cfg, err := LoadConfigForEdit(path)
		require.NoError(t, err)
		assert.Equal(t, want.Agents.Defaults.MaxTokens, cfg.Agents.Defaults.MaxTokens)
		assert.Equal(t, want.Agents.Defaults.Workspace, cfg.Agents.Defaults.Workspace)
		assert.Equal(t, "{}", readRaw(t, path), "loading must not rewrite the file")
	})
}

func TestLoadConfigForEdit_MigratesLegacyVersionWithoutEnv(t *testing.T) {
	path, _ := editTestPaths(t)
	raw := `{
		"version": 2,
		"agents": {
			"defaults": {
				"workspace": "~/.kuromatsu/workspace",
				"max_tokens": 32768
			}
		}
	}`
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	t.Setenv(envMaxTokens, "1234")

	cfg, err := LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.Equal(t, CurrentVersion, cfg.Version)
	assert.Equal(t, 32768, cfg.Agents.Defaults.MaxTokens)

	var onDisk struct {
		Version int `json:"version"`
		Agents  struct {
			Defaults struct {
				MaxTokens int `json:"max_tokens"`
			} `json:"defaults"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal([]byte(readRaw(t, path)), &onDisk))
	assert.Equal(t, CurrentVersion, onDisk.Version)
	assert.Equal(t, 32768, onDisk.Agents.Defaults.MaxTokens)
}

// In a read-only directory the migration stops at its backup, before the
// rewrite: the error comes back once and the file is left as it was.
func TestLoadConfigForEdit_MigrationNotPersistedFailsOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directories are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"version": 2, "agents": {"defaults": {"max_tokens": 32768}}}`
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := LoadConfigForEdit(path)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "failed to create config backup"), "error = %v", err)
	assert.Equal(t, raw, readRaw(t, path), "the file must be left as it was")
}
