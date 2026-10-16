package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

type fakeMux struct{ mux *http.ServeMux }

func (f *fakeMux) HandleHTTP(pattern string, h http.Handler) error {
	f.mux.Handle(pattern, h)
	return nil
}

func captureLog(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "webui.log")
	require.NoError(t, logger.EnableFileLogging(p))
	t.Cleanup(logger.DisableFileLogging)
	return p
}

func TestWebUI_NoTokenDoesNotMountOrWrite(t *testing.T) {
	e := newTestEnv(t)
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	cfg.WebUI.Token = config.SecureString{}
	require.NoError(t, config.SaveConfig(e.configPath, cfg))
	secPath := filepath.Join(e.home, config.SecurityConfigFile)
	cfgInfo, err := os.Stat(e.configPath)
	require.NoError(t, err)
	secInfo, err := os.Stat(secPath)
	require.NoError(t, err)
	cfgBefore, secBefore := readFile(t, e.configPath), readFile(t, secPath)

	logPath := captureLog(t)
	rt, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	require.True(t, rt.WebUI.Enabled)
	m := &fakeMux{mux: http.NewServeMux()}
	mounted, err := Mount(m, rt, e.deps())
	require.NoError(t, err)
	assert.False(t, mounted)

	rec := httptest.NewRecorder()
	m.mux.ServeHTTP(rec, req(http.MethodGet, "/ui/", ""))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, readFile(t, logPath), "web UI disabled: no token; run 'kuromatsu ui token'")

	assert.Equal(t, cfgBefore, readFile(t, e.configPath))
	assert.Equal(t, secBefore, readFile(t, secPath))
	after, err := os.Stat(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, cfgInfo.ModTime(), after.ModTime())
	secAfter, err := os.Stat(secPath)
	require.NoError(t, err)
	assert.Equal(t, secInfo.ModTime(), secAfter.ModTime())
}

func TestWebUI_MountEnabledDisabledAndRemoteWarn(t *testing.T) {
	e := newTestEnv(t)
	rt, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)

	m := &fakeMux{mux: http.NewServeMux()}
	mounted, err := Mount(m, rt, e.deps())
	require.NoError(t, err)
	require.True(t, mounted)
	rec := httptest.NewRecorder()
	m.mux.ServeHTTP(rec, req(http.MethodGet, "/ui/", ""))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"))

	rt.WebUI.Enabled = false
	off := &fakeMux{mux: http.NewServeMux()}
	mounted, err = Mount(off, rt, e.deps())
	require.NoError(t, err)
	assert.False(t, mounted)
	rec = httptest.NewRecorder()
	off.mux.ServeHTTP(rec, req(http.MethodGet, "/ui/", ""))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	logPath := captureLog(t)
	rt.WebUI.Enabled = true
	rt.WebUI.AllowRemote = true
	remote := &fakeMux{mux: http.NewServeMux()}
	mounted, err = Mount(remote, rt, e.deps())
	require.NoError(t, err)
	require.True(t, mounted)
	assert.Contains(t, readFile(t, logPath), "web UI reachable from the network over plain HTTP; prefer an SSH tunnel")
	r := bearer(req(http.MethodGet, "/api/ui/models", ""))
	r.RemoteAddr = "192.168.0.10:5555"
	rec = httptest.NewRecorder()
	remote.mux.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNew_RefusesEmptyToken(t *testing.T) {
	_, err := New(Deps{})
	assert.Error(t, err)
}
