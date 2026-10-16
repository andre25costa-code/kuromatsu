package webui

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestConfigRoundTrip_NoEnvLeak(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_TOKEN", "env-tok")
	t.Setenv("KUROMATSU_AGENTS_DEFAULTS_MODEL_NAME", "env-model")
	secPath := filepath.Join(e.home, config.SecurityConfigFile)

	rec := e.do(bearer(req(http.MethodPut, "/api/ui/heartbeat", `{"enabled":true,"interval":45}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int32(1), e.reloads.Load())

	raw := readFile(t, e.configPath)
	sec := readFile(t, secPath)
	for _, leaked := range []string{"env-tok", "env-model"} {
		assert.NotContains(t, raw, leaked)
		assert.NotContains(t, sec, leaked)
	}
	assert.Contains(t, sec, testTGToken, "existing secrets are kept")
	assert.Contains(t, sec, testModelKey)
	assert.Contains(t, sec, testToken)
	assert.NotContains(t, raw, testTGToken)
	assert.NotContains(t, raw, testToken)

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, 45, cfg.Heartbeat.Interval)
	assert.True(t, cfg.Heartbeat.Enabled)
	assert.Equal(t, "mine", cfg.Agents.Defaults.ModelName)
}

func TestConfigWrite_InvalidIsRejected(t *testing.T) {
	e := newTestEnv(t)
	before := readFile(t, e.configPath)
	cases := []struct{ method, path, body string }{
		{http.MethodPut, "/api/ui/channels/telegram/allow_from", `{"allow_from":[]}`},
		{http.MethodPut, "/api/ui/channels/telegram/allow_from", `{"allow_from":["  "]}`},
		{http.MethodPut, "/api/ui/heartbeat", `{"enabled":true,"interval":4}`},
		{http.MethodPut, "/api/ui/models/default", `{"alias":"nope"}`},
		{http.MethodPut, "/api/ui/permissions", `{"preset":"root"}`},
		{http.MethodPost, "/api/ui/models", `{"alias":"x","model":""}`},
	}
	for _, c := range cases {
		rec := e.do(bearer(req(c.method, c.path, c.body)))
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%s %s: %s", c.method, c.path, rec.Body.String())
	}
	// A config change that fails validation as a whole (an invalid
	// timezone written by hand is the stand-in: no route sets it) is caught
	// by ValidateForSave and nothing is written.
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	cfg.Timezone = "Mars/Olympus"
	require.NoError(t, config.SaveConfig(e.configPath, cfg))
	broken := readFile(t, e.configPath)
	rec := e.do(bearer(req(http.MethodPut, "/api/ui/heartbeat", `{"enabled":true,"interval":30}`)))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "invalid config")
	assert.Equal(t, broken, readFile(t, e.configPath))

	assert.Equal(t, int32(0), e.reloads.Load())
	assert.NotEqual(t, before, broken)
	assert.Equal(t, http.StatusNotFound,
		e.do(bearer(req(http.MethodPut, "/api/ui/channels/nope/allow_from", `{"allow_from":["1"]}`))).Code)
	assert.Equal(t, http.StatusBadRequest,
		e.do(bearer(req(http.MethodPut, "/api/ui/heartbeat", `{"enabled":true,"interval":30,"x":1}`))).Code)
}

func TestConfigWrite_AllowFromUnchangedOnReject(t *testing.T) {
	e := newTestEnv(t)
	before := readFile(t, e.configPath)
	rec := e.do(bearer(req(http.MethodPut, "/api/ui/channels/telegram/allow_from", `{"allow_from":[]}`)))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, before, readFile(t, e.configPath))

	rec = e.do(bearer(req(http.MethodPut, "/api/ui/channels/telegram/allow_from", `{"allow_from":["111"," 222 ",""]}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"111", "222"}, []string(cfg.Channels["telegram"].AllowFrom))
	assert.Contains(t, readFile(t, filepath.Join(e.home, config.SecurityConfigFile)), testTGToken)
}

func TestAPI_NoSecretsInResponses(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("KUROMATSU_CHANNELS_TELEGRAM_TOKEN", "env-tok")
	for _, p := range []string{
		"/api/ui/models", "/api/ui/channels", "/api/ui/status", "/api/ui/permissions",
		"/api/ui/heartbeat", "/api/ui/cron/templates", "/api/ui/session",
	} {
		rec := e.do(bearer(req(http.MethodGet, p, "")))
		require.Equal(t, http.StatusOK, rec.Code, p)
		body := rec.Body.String()
		for _, secret := range []string{testModelKey, testTGToken, testToken, "env-tok"} {
			assert.NotContains(t, body, secret, p)
		}
	}
	rec := e.do(bearer(req(http.MethodGet, "/api/ui/models", "")))
	assert.Contains(t, rec.Body.String(), `"has_key":true`)
}

func TestModels_AddKeepsKeyAndSetsDefault(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(bearer(req(http.MethodPost, "/api/ui/models",
		`{"alias":"mine","provider":"","base":"http://127.0.0.1:9/v1","key":"","model":"model-y"}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = e.do(bearer(req(http.MethodPost, "/api/ui/models",
		`{"alias":"other","provider":"ollama","base":"http://127.0.0.1:11434/v1","key":"","model":"llama"}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, "other", cfg.Agents.Defaults.ModelName)
	for _, m := range cfg.ModelList {
		if m.ModelName == "mine" {
			assert.Equal(t, "model-y", m.Model)
			assert.Equal(t, testModelKey, m.APIKey(), "blank key keeps the stored one")
		}
	}

	rec = e.do(bearer(req(http.MethodPut, "/api/ui/models/default", `{"alias":"mine"}`)))
	require.Equal(t, http.StatusOK, rec.Code)
	cfg, err = config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, "mine", cfg.Agents.Defaults.ModelName)
	assert.Equal(t, int32(3), e.reloads.Load())
}

func TestPermissions_PresetAndCustom(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(bearer(req(http.MethodPut, "/api/ui/permissions", `{"preset":"seguro"}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = e.do(bearer(req(http.MethodGet, "/api/ui/permissions", "")))
	assert.Contains(t, rec.Body.String(), `"preset":"seguro"`)

	rec = e.do(
		bearer(
			req(http.MethodPut, "/api/ui/permissions", `{"custom":{"restrict_to_workspace":true,"web_enabled":true}}`),
		),
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = e.do(bearer(req(http.MethodGet, "/api/ui/permissions", "")))
	assert.Contains(t, rec.Body.String(), `"preset":"custom"`)
	assert.Equal(t, http.StatusUnprocessableEntity,
		e.do(bearer(req(http.MethodPut, "/api/ui/permissions", `{"preset":"seguro","custom":{}}`))).Code)
}

// The reload trigger answering "already in progress" is not an error.
func TestConfigWrite_ReloadBusyIs202(t *testing.T) {
	e := newTestEnv(t)
	d := e.deps()
	d.Reload = func() error { return errBusy }
	srv, err := newServer(d)
	require.NoError(t, err)
	rec := newRecorder()
	srv.ServeHTTP(rec, bearer(req(http.MethodPut, "/api/ui/heartbeat", `{"enabled":false,"interval":30}`)))
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "reload already in progress"))
}

type busyError struct{}

func (busyError) Error() string { return "reload already in progress" }

var errBusy error = busyError{}
