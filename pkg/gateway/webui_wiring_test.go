package gateway

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/channels"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/health"
	"github.com/andre25costa-code/kuromatsu/pkg/webui"
)

// startSharedServer runs a channel manager's shared HTTP server on a free
// loopback port with the health endpoints, mounts the web UI per cfg and
// returns the base URL. POST /reload reloads the channel manager, as the
// gateway's reload does.
func startSharedServer(t *testing.T, cfg *config.Config, configPath string) (string, bool) {
	t.Helper()
	msgBus := bus.NewMessageBus()
	t.Cleanup(msgBus.Close)
	cm, err := channels.NewManager(cfg, msgBus, nil)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	hs := health.NewServer("127.0.0.1", port, "reload-token")
	hs.SetReloadFunc(func() error { return cm.Reload(context.Background(), cfg) })
	cm.SetupHTTPServerListeners([]net.Listener{ln}, ln.Addr().String(), hs)

	d := webui.Deps{
		ConfigPath: configPath,
		Workspace:  func() string { return t.TempDir() },
		Reload:     func() error { return nil },
		Now:        time.Now,
	}
	mounted := mountWebUI(cm, cfg, d)
	require.NoError(t, cm.StartAll(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cm.StopAll(ctx)
	})
	return "http://" + ln.Addr().String(), mounted
}

// get returns the status and headers of GET url.
func get(t *testing.T, url string) (int, http.Header) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header
}

func TestGateway_MountsWebUIAndSurvivesReload(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	path := filepath.Join(home, "config.json")
	cfg := config.DefaultConfig()
	_, err := cfg.WebUI.EnsureToken()
	require.NoError(t, err)
	require.NoError(t, config.SaveConfig(path, cfg))

	base, mounted := startSharedServer(t, cfg, path)
	require.True(t, mounted)

	code, hdr := get(t, base+"/ui/")
	require.Equal(t, http.StatusOK, code)
	assert.NotEmpty(t, hdr.Get("Content-Security-Policy"))

	r, err := http.NewRequest(http.MethodPost, base+"/reload", nil)
	require.NoError(t, err)
	r.Header.Set("Authorization", "Bearer reload-token")
	reloadResp, err := http.DefaultClient.Do(r)
	require.NoError(t, err)
	reloadResp.Body.Close()
	require.Equal(t, http.StatusOK, reloadResp.StatusCode)

	assert.Equal(t, http.StatusOK, status(t, base+"/ui/"), "after /reload")
	assert.Equal(t, http.StatusUnauthorized, status(t, base+"/api/ui/models"))
}

func TestGateway_WebUIDisabledOrWithoutTokenIs404(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	path := filepath.Join(home, "config.json")

	off := config.DefaultConfig()
	_, err := off.WebUI.EnsureToken()
	require.NoError(t, err)
	off.WebUI.Enabled = false
	base, mounted := startSharedServer(t, off, path)
	assert.False(t, mounted)
	assert.Equal(t, http.StatusNotFound, status(t, base+"/ui/"))

	noToken := config.DefaultConfig()
	base, mounted = startSharedServer(t, noToken, path)
	assert.False(t, mounted)
	assert.Equal(t, http.StatusNotFound, status(t, base+"/ui/"))
	assert.Equal(t, http.StatusNotFound, status(t, base+"/api/ui/models"))
}

func status(t *testing.T, url string) int {
	t.Helper()
	code, _ := get(t, url)
	return code
}
