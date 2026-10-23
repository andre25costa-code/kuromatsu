package model

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

func TestFetchOpenAIModels_DataEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/models", r.URL.Path)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-foo","name":"Foo"},{"id":"gpt-bar"}]}`))
	}))
	defer srv.Close()

	entries, err := fetchModels(setup.KindOpenAI, srv.URL, "secret")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "gpt-foo", entries[0].ID)
	assert.Equal(t, "Foo", entries[0].Name)
	assert.Equal(t, "gpt-bar", entries[1].ID)
}

func TestFetchOpenAIModels_BareArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"a"},{"id":"b"}]`))
	}))
	defer srv.Close()

	entries, err := fetchModels(setup.KindOpenAI, srv.URL, "secret")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "a", entries[0].ID)
	assert.Equal(t, "b", entries[1].ID)
}

func TestFetchOpenAIModels_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":"x"}]}`))
	}))
	defer srv.Close()

	_, err := fetchModels(setup.KindOpenAI, srv.URL+"/", "k")
	require.NoError(t, err)
	assert.Equal(t, "/models", gotPath)
}

func TestFetchOpenAIModels_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := fetchModels(setup.KindOpenAI, srv.URL, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401")
}

func TestFetchOpenAIModels_HTTPErrorReadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := "short"
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, err := fetchModels(setup.KindOpenAI, srv.URL, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read error response")
	assert.Contains(t, err.Error(), "unexpected EOF")
}

func TestFetchOpenAIModels_EmptyDataEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	entries, err := fetchModels(setup.KindOpenAI, srv.URL, "k")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestFetchOpenAIModels_EmptyBareArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	entries, err := fetchModels(setup.KindOpenAI, srv.URL, "k")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestFetchOpenAIModels_UnrecognizedShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":"not-supported"}`))
	}))
	defer srv.Close()

	_, err := fetchModels(setup.KindOpenAI, srv.URL, "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognized shape")
}

func TestFetchOpenAIModels_RequiresInputs(t *testing.T) {
	_, err := fetchModels(setup.KindOpenAI, "", "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api base")

	_, err = fetchModels(setup.KindOpenAI, "https://example.com", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api key")
}

func TestPickModel_ByIndex(t *testing.T) {
	entries := []modelEntry{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	out := &bytes.Buffer{}
	got, err := pickModel(strings.NewReader("2\n"), out, entries)
	require.NoError(t, err)
	assert.Equal(t, "b", got)
	assert.Contains(t, out.String(), "3 model(s) available")
}

func TestPickModel_ByID(t *testing.T) {
	entries := []modelEntry{{ID: "alpha"}, {ID: "beta"}}
	out := &bytes.Buffer{}
	got, err := pickModel(strings.NewReader("beta\n"), out, entries)
	require.NoError(t, err)
	assert.Equal(t, "beta", got)
}

func TestPickModel_RetriesOnInvalid(t *testing.T) {
	entries := []modelEntry{{ID: "x"}}
	out := &bytes.Buffer{}
	got, err := pickModel(strings.NewReader("\n9\nnot-a-model\nx\n"), out, entries)
	require.NoError(t, err)
	assert.Equal(t, "x", got)
	rendered := out.String()
	assert.Contains(t, rendered, "Out of range")
	assert.Contains(t, rendered, "Not a valid number")
}

func TestRunAdd_WithExplicitModel_NoNetwork(t *testing.T) {
	initTest(t)

	out := &bytes.Buffer{}
	err := runAdd(addOptions{
		apiBase:   "https://invalid.invalid/v1",
		apiKey:    "k",
		modelID:   "explicit-model",
		alias:     "myalias",
		modelType: "openai-compatible",
		stdout:    out,
	})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Saved model 'myalias' (explicit-model)")

	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, "myalias", cfg.Agents.Defaults.GetModelName())
	added := findModelByName(cfg, "myalias")
	require.NotNil(t, added, "expected model 'myalias' in model_list")
	assert.Equal(t, "explicit-model", added.Model)
	assert.Equal(t, "https://invalid.invalid/v1", added.APIBase)
	assert.True(t, added.Enabled)
	require.Len(t, added.APIKeys, 1)
	assert.Equal(t, "k", added.APIKeys[0].String())
}

func findModelByName(cfg *config.Config, name string) *config.ModelConfig {
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == name {
			return m
		}
	}
	return nil
}

func TestRunAdd_FetchAndPick(t *testing.T) {
	initTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer my-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	}))
	defer srv.Close()

	out := &bytes.Buffer{}
	err := runAdd(addOptions{
		apiBase:   srv.URL,
		apiKey:    "my-key",
		alias:     defaultAliasName,
		modelType: "openai-compatible",
		stdin:     strings.NewReader("2\n"),
		stdout:    out,
	})
	require.NoError(t, err)

	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, defaultAliasName, cfg.Agents.Defaults.GetModelName())
	added := findModelByName(cfg, defaultAliasName)
	require.NotNil(t, added)
	assert.Equal(t, "m2", added.Model)
}

func TestRunAdd_UpsertsExistingAlias(t *testing.T) {
	initTest(t)

	first := &bytes.Buffer{}
	require.NoError(t, runAdd(addOptions{
		apiBase: "https://a.example/v1",
		apiKey:  "k1",
		modelID: "m1",
		alias:   "shared",
		stdout:  first,
	}))

	second := &bytes.Buffer{}
	require.NoError(t, runAdd(addOptions{
		apiBase: "https://b.example/v1",
		apiKey:  "k2",
		modelID: "m2",
		alias:   "shared",
		stdout:  second,
	}))

	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	matches := 0
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == "shared" {
			matches++
		}
	}
	assert.Equal(t, 1, matches, "alias should be updated, not duplicated")

	updated := findModelByName(cfg, "shared")
	require.NotNil(t, updated)
	assert.Equal(t, "m2", updated.Model)
	assert.Equal(t, "https://b.example/v1", updated.APIBase)
	assert.Equal(t, "k2", updated.APIKeys[0].String())
}

func TestRunAdd_RejectsUnsupportedType(t *testing.T) {
	initTest(t)

	err := runAdd(addOptions{
		apiBase:   "https://x/v1",
		apiKey:    "k",
		modelID:   "m",
		alias:     "a",
		modelType: "anthropic",
		stdout:    &bytes.Buffer{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported --type")
}

func TestRunAdd_DoesNotPersistEnv(t *testing.T) {
	initTest(t)

	seed := config.DefaultConfig()
	seed.Agents.Defaults.MaxTokens = 32768
	require.NoError(t, config.SaveConfig(configPath, seed))

	t.Setenv("KUROMATSU_AGENTS_DEFAULTS_MAX_TOKENS", "1234")

	out := &bytes.Buffer{}
	require.NoError(t, runAdd(addOptions{
		apiBase:   "https://invalid.invalid/v1",
		apiKey:    "k",
		modelID:   "explicit-model",
		alias:     "myalias",
		modelType: "openai-compatible",
		stdout:    out,
	}))

	modelName, maxTokens := rawDefaults(t, configPath)
	assert.Equal(t, "myalias", modelName)
	assert.Equal(t, 32768, maxTokens)
}

func TestRunAdd_9RouterDefaultsAndProvider(t *testing.T) {
	initTest(t)
	t.Setenv(config.EnvHome, t.TempDir())

	out := &bytes.Buffer{}
	require.NoError(t, runAdd(addOptions{
		modelID:   "glm/glm-5.1",
		alias:     "router",
		modelType: "9router",
		stdout:    out,
	}))

	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	added := findModelByName(cfg, "router")
	require.NotNil(t, added)
	assert.Equal(t, "9router", added.Provider)
	assert.Equal(t, "http://localhost:20128/v1", added.APIBase)
	assert.Equal(t, "glm/glm-5.1", added.Model)
	assert.Empty(t, added.APIKeys)
	assert.Equal(t, "router", cfg.Agents.Defaults.GetModelName())
}

func TestRunAdd_9RouterProbeWithoutKey(t *testing.T) {
	initTest(t)
	t.Setenv(config.EnvHome, t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present := r.Header["Authorization"]
		assert.False(t, present, "no key: the probe must not send Authorization")
		assert.Equal(t, "/models", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[{"id":"cc/claude-opus-4-7"},{"id":"premium-coding"}]}`))
	}))
	defer srv.Close()

	require.NoError(t, runAdd(addOptions{
		apiBase:   srv.URL,
		alias:     "router",
		modelType: "9router",
		stdin:     strings.NewReader("2\n"),
		stdout:    &bytes.Buffer{},
	}))

	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	added := findModelByName(cfg, "router")
	require.NotNil(t, added)
	assert.Equal(t, "9router", added.Provider)
	assert.Equal(t, "premium-coding", added.Model)
	assert.Equal(t, srv.URL, added.APIBase)
}

func TestRunAdd_OpenAICompatibleStillRequiresBaseAndKey(t *testing.T) {
	initTest(t)
	t.Setenv(config.EnvHome, t.TempDir())

	for _, modelType := range []string{"", "openai-compatible"} {
		err := runAdd(addOptions{apiKey: "k", modelID: "m", modelType: modelType, stdout: &bytes.Buffer{}})
		require.Error(t, err, modelType)
		assert.Contains(t, err.Error(), "--api-base is required")

		err = runAdd(addOptions{
			apiBase: "https://x/v1", modelID: "m", modelType: modelType, stdout: &bytes.Buffer{},
		})
		require.Error(t, err, modelType)
		assert.Contains(t, err.Error(), "--api-key is required")
	}
}

func TestRunAdd_9RouterOverridesExistingProvider(t *testing.T) {
	initTest(t)
	t.Setenv(config.EnvHome, t.TempDir())

	require.NoError(t, runAdd(addOptions{
		apiBase: "https://a.example/v1", apiKey: "k1", modelID: "m1", alias: "shared", stdout: &bytes.Buffer{},
	}))
	cfg, err := config.LoadConfigForEdit(configPath)
	require.NoError(t, err)
	findModelByName(cfg, "shared").Provider = "openrouter"
	require.NoError(t, config.SaveConfig(configPath, cfg))

	require.NoError(t, runAdd(addOptions{
		modelID: "kr/claude-sonnet-4.5", alias: "shared", modelType: "9router", stdout: &bytes.Buffer{},
	}))
	cfg, err = config.LoadConfig(configPath)
	require.NoError(t, err)
	updated := findModelByName(cfg, "shared")
	require.NotNil(t, updated)
	assert.Equal(t, "9router", updated.Provider)
	assert.Equal(t, "kr/claude-sonnet-4.5", updated.Model)
}

func TestRunAdd_OpenAICompatibleClears9RouterProvider(t *testing.T) {
	initTest(t)
	t.Setenv(config.EnvHome, t.TempDir())

	require.NoError(t, runAdd(addOptions{
		modelID: "glm/glm-5.1", alias: "router", modelType: "9router", stdout: &bytes.Buffer{},
	}))
	require.NoError(t, runAdd(addOptions{
		apiBase: "https://api.example/v1", apiKey: "k", modelID: "gpt-x", alias: "router",
		modelType: "openai-compatible", stdout: &bytes.Buffer{},
	}))
	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	m := findModelByName(cfg, "router")
	require.NotNil(t, m)
	assert.Empty(t, m.Provider, "openai-compatible must not keep provider 9router")

	// Any other provider is still preserved.
	edit, err := config.LoadConfigForEdit(configPath)
	require.NoError(t, err)
	findModelByName(edit, "router").Provider = "openrouter"
	require.NoError(t, config.SaveConfig(configPath, edit))
	require.NoError(t, runAdd(addOptions{
		apiBase: "https://openrouter.ai/api/v1", apiKey: "k", modelID: "a/b", alias: "router",
		stdout: &bytes.Buffer{},
	}))
	cfg, err = config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, "openrouter", findModelByName(cfg, "router").Provider)
}

func TestNewAddCommand_TypeFlag(t *testing.T) {
	cmd := newAddCommand()
	typeFlag := cmd.Flags().Lookup("type")
	require.NotNil(t, typeFlag)
	assert.Contains(t, typeFlag.Usage, "openai-compatible")
	assert.Contains(t, typeFlag.Usage, "9router")
	for _, name := range []string{"api-base", "api-key"} {
		f := cmd.Flags().Lookup(name)
		require.NotNil(t, f, name)
		_, required := f.Annotations[cobra.BashCompOneRequiredFlag]
		assert.False(t, required, "%s must not be a cobra-required flag", name)
	}
	assert.Contains(t, cmd.Example, "kuromatsu model add --type 9router")
	assert.Contains(t, cmd.Example, "kuromatsu model add --type 9router -m cc/claude-opus-4-7 -n router")

	help := &bytes.Buffer{}
	cmd.SetOut(help)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, help.String(), "'openai-compatible' or '9router'")
	assert.Contains(t, help.String(), "--type 9router -m cc/claude-opus-4-7 -n router")
}
