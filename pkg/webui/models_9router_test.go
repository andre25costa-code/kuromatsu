package webui

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

func TestModels_9RouterOptionAndProbeKind(t *testing.T) {
	assert.Equal(t, setup.Kind9Router, probeKind("9router"))
	html, err := fs.ReadFile(staticFiles, "static/index.html")
	require.NoError(t, err)
	assert.Contains(t, string(html), `value="9router"`)
}

// With 9router, a blank base means the default local address, both for the
// connection test and for the saved entry.
func TestModels_9RouterEmptyBaseUsesDefault(t *testing.T) {
	e := newTestEnv(t)
	old := probeModelList
	t.Cleanup(func() { probeModelList = old })
	var gotKind, gotBase, gotKey string
	probeModelList = func(_ context.Context, kind, base, key string) ([]setup.ModelEntry, error) {
		gotKind, gotBase, gotKey = kind, base, key
		return []setup.ModelEntry{{ID: "cc/claude-opus-4-7"}}, nil
	}

	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	e.srv.ServeHTTP(rec, bearer(req(http.MethodPost, "/api/ui/models/probe",
		`{"provider":"9router","base":"","key":"","alias":"router"}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, setup.Kind9Router, gotKind)
	assert.Equal(t, "http://localhost:20128/v1", gotBase)
	assert.Empty(t, gotKey)
	assert.Contains(t, rec.Body.String(), "cc/claude-opus-4-7")

	add := e.do(bearer(req(http.MethodPost, "/api/ui/models",
		`{"alias":"router","provider":"9router","base":"","key":"","model":"cc/claude-opus-4-7"}`)))
	require.Equal(t, http.StatusOK, add.Code, add.Body.String())
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	var saved *config.ModelConfig
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == "router" {
			saved = m
		}
	}
	require.NotNil(t, saved)
	assert.Equal(t, "9router", saved.Provider)
	assert.Equal(t, "http://localhost:20128/v1", saved.APIBase)
	assert.Equal(t, "cc/claude-opus-4-7", saved.Model)
	assert.Empty(t, saved.APIKeys)
	assert.Equal(t, "router", cfg.Agents.Defaults.ModelName)
}

// The connection test without a key reaches 9router with no Authorization.
func TestModels_9RouterProbeWithoutKeySendsNoAuth(t *testing.T) {
	e := newTestEnv(t)
	authSeen := "unset"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen = "<absent>"
		if v, ok := r.Header["Authorization"]; ok {
			authSeen = v[0]
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"glm/glm-5.1"}]}`))
	}))
	t.Cleanup(srv.Close)

	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	e.srv.ServeHTTP(rec, bearer(req(http.MethodPost, "/api/ui/models/probe",
		`{"provider":"9router","base":"`+srv.URL+`","key":""}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "<absent>", authSeen)
	assert.Contains(t, rec.Body.String(), "glm/glm-5.1")
}
