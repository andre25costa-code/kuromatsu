package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// writeTelegramConfig saves a config with a telegram channel whose token
// lives in the security file, and returns the config path.
func writeTelegramConfig(t *testing.T, token string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := config.DefaultConfig()
	tg := &config.Channel{Type: config.ChannelTelegram, Enabled: true, AllowFrom: config.FlexibleStringSlice{"123"}}
	tg.SetName(config.ChannelTelegram)
	tg.Settings = config.RawNode("{}")
	decoded, err := tg.GetDecoded()
	require.NoError(t, err)
	decoded.(*config.TelegramSettings).Token = *config.NewSecureString(token)
	cfg.Channels = config.ChannelsConfig{config.ChannelTelegram: tg}
	cfg.Heartbeat.Interval = 30
	require.NoError(t, config.SaveConfig(path, cfg))
	return path
}

func readRaw(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// The edit path loads the file as stored, so an environment override is never
// written to disk, and the token already in the security file survives.
func TestSetKey_NoEnvLeak(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	t.Setenv("KUROMATSU_KEY_PASSPHRASE", "")
	const fileToken = "111111:file-token-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	const envToken = "222222:env-token-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	path := writeTelegramConfig(t, fileToken)
	secPath := filepath.Join(filepath.Dir(path), config.SecurityConfigFile)
	require.Contains(t, readRaw(t, secPath), fileToken)

	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_TOKEN", envToken)
	t.Setenv("KUROMATSU_HEARTBEAT_INTERVAL", "90")

	cfg, err := config.LoadConfigForEdit(path)
	require.NoError(t, err)
	oldV, newV, err := SetKey(cfg, "heartbeat.interval", "45")
	require.NoError(t, err)
	assert.Equal(t, "30", oldV)
	assert.Equal(t, "45", newV)
	require.NoError(t, config.ValidateForSave(cfg))
	require.NoError(t, config.SaveConfig(path, cfg))

	// The same through SaveKey, the pipeline the chat command and the web UI use.
	_, _, err = SaveKey(path, "heartbeat.enabled", "on")
	require.NoError(t, err)

	for _, p := range []string{path, secPath} {
		raw := readRaw(t, p)
		assert.NotContains(t, raw, envToken, p)
	}
	assert.Contains(t, readRaw(t, secPath), fileToken)
	assert.NotContains(t, readRaw(t, path), fileToken)
	assert.NotContains(t, readRaw(t, path), "[NOT_HERE]")

	got, err := config.LoadConfigForEdit(path)
	require.NoError(t, err)
	assert.Equal(t, 45, got.Heartbeat.Interval)
	assert.True(t, got.Heartbeat.Enabled)
}

func TestSetKey_Validation(t *testing.T) {
	cases := []struct {
		key, value string
		wantErr    string
	}{
		{"heartbeat.interval", "3", "minimum 5"},
		{"heartbeat.interval", "x", "invalid number"},
		{"heartbeat.enabled", "maybe", "on, off"},
		{"agents.defaults.user_turn_max_minutes", "-1", "minimum 0"},
		{"gateway.log_level", "fatal", "invalid log level"},
		{"timezone", "Mars/Olympus", "unknown timezone"},
		{"timezone", "Local", "not a timezone name"},
		{"tools.exec.allow_remote", "true", "not settable from chat"},
		{"channel_list.telegram.allow_from", "*", "not settable from chat"},
	}
	for _, tc := range cases {
		cfg := config.DefaultConfig()
		before := *cfg
		_, _, err := SetKey(cfg, tc.key, tc.value)
		require.Error(t, err, "%s=%s", tc.key, tc.value)
		assert.Contains(t, err.Error(), tc.wantErr, "%s=%s", tc.key, tc.value)
		assert.Equal(t, before.Heartbeat, cfg.Heartbeat)
		assert.Equal(t, before.Timezone, cfg.Timezone)
		assert.Equal(t, before.Gateway, cfg.Gateway)
	}

	ok := []struct{ key, value, want string }{
		{"heartbeat.enabled", "off", "off"},
		{"heartbeat.enabled", "TRUE", "on"},
		{"heartbeat.interval", "5", "5"},
		{"agents.defaults.user_turn_max_minutes", "0", "0"},
		{"tools.web.duckduckgo.enabled", "on", "on"},
		{"gateway.log_level", "INFO", "info"},
		{"timezone", "America/Sao_Paulo", "America/Sao_Paulo"},
		{"timezone", "", ""},
	}
	for _, tc := range ok {
		cfg := config.DefaultConfig()
		_, got, err := SetKey(cfg, tc.key, tc.value)
		require.NoError(t, err, "%s=%s", tc.key, tc.value)
		assert.Equal(t, tc.want, got)
	}
	for _, key := range ChatSettableKeys {
		assert.True(t, IsChatSettable(key))
		assert.False(t, strings.Contains(key, "allow_from") || strings.Contains(key, "exec"), key)
	}
}

// A failing step writes nothing.
func TestSaveKey_InvalidValueWritesNothing(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	t.Setenv("KUROMATSU_KEY_PASSPHRASE", "")
	path := writeTelegramConfig(t, "111111:file-token-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	before := readRaw(t, path)

	_, _, err := SaveKey(path, "heartbeat.interval", "3")
	require.ErrorContains(t, err, "minimum 5")
	_, _, err = SaveKey(path, "tools.exec.allow_remote", "true")
	require.ErrorIs(t, err, ErrKeyNotSettable)
	assert.Equal(t, before, readRaw(t, path))
}
