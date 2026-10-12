package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestNewLogoutSubcommand(t *testing.T) {
	cmd := newLogoutCommand()

	require.NotNil(t, cmd)

	assert.Equal(t, "Remove stored credentials", cmd.Short)

	assert.True(t, cmd.HasFlags())

	assert.NotNil(t, cmd.Flags().Lookup("provider"))
}

func TestAuthLogoutAll_DoesNotPersistEnv(t *testing.T) {
	// Keep the credential store and the config inside the test directory.
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	t.Setenv(config.EnvHome, home)
	t.Setenv(config.EnvConfig, configPath)

	seed := config.DefaultConfig()
	seed.Agents.Defaults.MaxTokens = 32768
	seed.ModelList = append(seed.ModelList, &config.ModelConfig{
		ModelName:  "oauth-model",
		Model:      "openai/gpt-5.4",
		AuthMethod: "oauth",
	})
	require.NoError(t, config.SaveConfig(configPath, seed))

	t.Setenv("KUROMATSU_AGENTS_DEFAULTS_MAX_TOKENS", "1234")

	require.NoError(t, authLogoutCmd(""))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var doc struct {
		Agents struct {
			Defaults struct {
				MaxTokens int `json:"max_tokens"`
			} `json:"defaults"`
		} `json:"agents"`
		ModelList []struct {
			ModelName  string `json:"model_name"`
			AuthMethod string `json:"auth_method"`
		} `json:"model_list"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, 32768, doc.Agents.Defaults.MaxTokens)
	for _, m := range doc.ModelList {
		assert.Empty(t, m.AuthMethod, "model %q kept its auth method", m.ModelName)
	}
}
