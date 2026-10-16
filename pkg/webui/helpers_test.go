package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

const (
	testToken    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testTGToken  = "123456:telegram-secret-token"
	testModelKey = "sk-model-secret-key"
	testHost     = "localhost:18790"
	testOrigin   = "http://localhost:18790"
)

type testEnv struct {
	t          *testing.T
	home       string
	configPath string
	workspace  string
	srv        *server
	reloads    atomic.Int32
	cron       atomic.Pointer[cron.CronService]
	now        time.Time
}

// newTestEnv writes a config with an enabled Telegram channel (token in the
// security file), a keyed model and the web UI token, in a temp home.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	e := &testEnv{
		t:          t,
		home:       home,
		configPath: filepath.Join(home, "config.json"),
		workspace:  filepath.Join(home, "workspace"),
		now:        time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, os.MkdirAll(e.workspace, 0o755))

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = e.workspace
	cfg.Tools.Cron.Enabled = true
	cfg.ModelList = append(cfg.ModelList, &config.ModelConfig{
		ModelName: "mine", Model: "model-x", APIBase: "http://127.0.0.1:9/v1",
		APIKeys: config.SimpleSecureStrings(testModelKey), Enabled: true,
	})
	cfg.Agents.Defaults.ModelName = "mine"
	bc := &config.Channel{Enabled: true, Type: "telegram", Settings: config.RawNode(`{}`)}
	settings := &config.TelegramSettings{}
	require.NoError(t, bc.Decode(settings))
	settings.Token = *config.NewSecureString(testTGToken)
	bc.AllowFrom = config.FlexibleStringSlice{"987654"}
	cfg.Channels["telegram"] = bc
	cfg.WebUI.Token = *config.NewSecureString(testToken)
	require.NoError(t, config.SaveConfig(e.configPath, cfg))

	srv, err := newServer(e.deps())
	require.NoError(t, err)
	e.srv = srv
	return e
}

func (e *testEnv) deps() Deps {
	return Deps{
		ConfigPath: e.configPath,
		Workspace:  func() string { return e.workspace },
		Runtime: func() *config.Config {
			cfg, err := config.LoadConfigForEdit(e.configPath)
			if err != nil {
				e.t.Errorf("runtime config: %v", err)
			}
			return cfg
		},
		Cron:   func() *cron.CronService { return e.cron.Load() },
		Reload: func() error { e.reloads.Add(1); return nil },
		Hardware: func() sysinfo.Hardware {
			return sysinfo.Hardware{TotalRAM: 2 << 30, CPUs: 2, Arch: "amd64", OS: "linux"}
		},
		Now:   func() time.Time { return e.now },
		Token: testToken,
	}
}

// req builds a request from loopback to the default Host.
func req(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = testHost
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func bearer(r *http.Request) *http.Request {
	r.Header.Set("Authorization", "Bearer "+testToken)
	return r
}

func (e *testEnv) do(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, r)
	return rec
}

// login signs in and returns the session cookie and CSRF token.
func (e *testEnv) login() (*http.Cookie, string) {
	e.t.Helper()
	r := req(http.MethodPost, "/api/ui/login", `{"token":"`+testToken+`"}`)
	r.Header.Set("Origin", testOrigin)
	rec := e.do(r)
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		CSRF string `json:"csrf"`
	}
	require.NoError(e.t, json.Unmarshal(rec.Body.Bytes(), &body))
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c, body.CSRF
		}
	}
	e.t.Fatal("no session cookie")
	return nil, ""
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	f, err := os.Open(p)
	require.NoError(t, err)
	defer f.Close()
	b, err := io.ReadAll(f)
	require.NoError(t, err)
	return string(b)
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
