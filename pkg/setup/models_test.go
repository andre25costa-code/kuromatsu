package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestProbeModels_HeadersPerProvider(t *testing.T) {
	cases := []struct {
		kind    string
		key     string
		check   func(t *testing.T, h http.Header)
		body    string
		wantIDs []string
	}{
		{
			kind: KindOpenAI, key: "sk-1",
			check: func(t *testing.T, h http.Header) {
				assert.Equal(t, "Bearer sk-1", h.Get("Authorization"))
			},
			body: `{"data":[{"id":"gpt-x"}]}`, wantIDs: []string{"gpt-x"},
		},
		{
			kind: KindOpenRouter, key: "or-1",
			check: func(t *testing.T, h http.Header) {
				assert.Equal(t, "Bearer or-1", h.Get("Authorization"))
			},
			body: `[{"id":"a/b"}]`, wantIDs: []string{"a/b"},
		},
		{
			kind: KindOllama,
			check: func(t *testing.T, h http.Header) {
				_, present := h["Authorization"]
				assert.False(t, present, "Ollama probe must not send Authorization")
			},
			body: `{"data":[{"id":"llama3"}]}`, wantIDs: []string{"llama3"},
		},
		{
			kind: KindAnthropic, key: "ant-1",
			check: func(t *testing.T, h http.Header) {
				assert.Equal(t, "ant-1", h.Get("X-Api-Key"))
				assert.Equal(t, AnthropicVersion, h.Get("Anthropic-Version"))
				assert.Empty(t, h.Get("Authorization"))
			},
			body:    `{"data":[{"id":"claude-x","display_name":"Claude X"}]}`,
			wantIDs: []string{"claude-x"},
		},
		{
			kind: KindGemini, key: "g-1",
			check: func(t *testing.T, h http.Header) {
				assert.Equal(t, "g-1", h.Get("X-Goog-Api-Key"))
				assert.Empty(t, h.Get("Authorization"))
			},
			body: `{"models":[` +
				`{"name":"models/gemini-2.5-flash","displayName":"Flash"},` +
				`{"name":"models/gemini-pro"}]}`,
			wantIDs: []string{"gemini-2.5-flash", "gemini-pro"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/models", r.URL.Path)
				tc.check(t, r.Header)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := ProbeModels(context.Background(), tc.kind, srv.URL+"/v1/", tc.key)
			require.NoError(t, err)
			ids := make([]string, 0, len(got))
			for _, m := range got {
				ids = append(ids, m.ID)
			}
			assert.Equal(t, tc.wantIDs, ids)
		})
	}
}

func TestProbeModels_KeyRequiredExceptOllama(t *testing.T) {
	for _, kind := range []string{KindOpenAI, KindOpenRouter, KindAnthropic, KindGemini} {
		_, err := ProbeModels(context.Background(), kind, "https://example.invalid", "")
		require.Error(t, err, kind)
		assert.Contains(t, err.Error(), "api key")
	}
	_, err := ProbeModels(context.Background(), "mystery", "https://example.invalid", "k")
	require.Error(t, err)
}

func TestProbeModels_HTTPErrorShowsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key"))
	}))
	defer srv.Close()

	_, err := ProbeModels(context.Background(), KindOpenAI, srv.URL, "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401: bad key")
}

func TestApplyModelChoice_UpsertsAndSetsDefault(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	cfg := config.DefaultConfig()
	before := len(cfg.ModelList)

	ApplyModelChoice(cfg, ModelChoice{
		Alias: "default", Provider: "anthropic", Model: "claude-x",
		APIBase: "https://api.anthropic.com/v1", APIKey: "ant-1",
	})
	require.Len(t, cfg.ModelList, before+1)
	m := cfg.ModelList[before]
	assert.Equal(t, "default", m.ModelName)
	assert.Equal(t, "anthropic", m.Provider)
	assert.Equal(t, "claude-x", m.Model)
	assert.True(t, m.Enabled)
	require.Len(t, m.APIKeys, 1)
	assert.Equal(t, "ant-1", m.APIKeys[0].String())
	assert.Equal(t, "default", cfg.Agents.Defaults.ModelName)

	// Same alias: updated in place; an empty key leaves no keys.
	ApplyModelChoice(cfg, ModelChoice{
		Alias: "default", Provider: "ollama", Model: "llama3", APIBase: "http://localhost:11434/v1",
	})
	require.Len(t, cfg.ModelList, before+1)
	assert.Equal(t, "ollama", m.Provider)
	assert.Equal(t, "llama3", m.Model)
	assert.Empty(t, m.APIKeys)
}

func TestProbeModels_9Router(t *testing.T) {
	cases := []struct {
		name     string
		key      string
		wantAuth string // "" = header absent
	}{
		{name: "no key", key: ""},
		{name: "with key", key: "router-key", wantAuth: "Bearer router-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/models", r.URL.Path)
				v, present := r.Header["Authorization"]
				if tc.wantAuth == "" {
					assert.False(t, present, "no key: Authorization must be absent, got %v", v)
				} else {
					assert.Equal(t, tc.wantAuth, r.Header.Get("Authorization"))
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"cc/claude-opus-4-7"},{"id":"premium-coding"}]}`))
			}))
			defer srv.Close()

			got, err := ProbeModels(context.Background(), Kind9Router, srv.URL+"/v1", tc.key)
			require.NoError(t, err)
			ids := make([]string, 0, len(got))
			for _, m := range got {
				ids = append(ids, m.ID)
			}
			assert.Equal(t, []string{"cc/claude-opus-4-7", "premium-coding"}, ids)
		})
	}
}
