package onboard

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/hwprofile"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// testToken is a well-formed bot token: digits, a colon, 35 characters.
var testToken = "123:" + strings.Repeat("A", 35)

var testHardware = sysinfo.Hardware{TotalRAM: 4 << 30, CPUs: 2, NativeCPUOK: true}

type wizardEnv struct {
	home       string
	configPath string
	out        *bytes.Buffer
}

// withZoneFiles points system-zone detection at a fake Linux root whose
// /etc/timezone names zone ("" = a root with no zone files).
func withZoneFiles(t *testing.T, zone string) string {
	t.Helper()
	root := t.TempDir()
	if zone != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "timezone"), []byte(zone+"\n"), 0o644))
	}
	oldRoot, oldGOOS := zoneRoot, zoneGOOS
	t.Cleanup(func() { zoneRoot, zoneGOOS = oldRoot, oldGOOS })
	zoneRoot, zoneGOOS = root, "linux"
	return root
}

// newWizardEnv isolates the wizard: temp home, fixed hardware, no TZ, a
// fake system zone (America/Sao_Paulo) and a fake Telegram API for getMe.
func newWizardEnv(t *testing.T) *wizardEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	configPath := filepath.Join(home, "config.json")
	t.Setenv(config.EnvConfig, configPath)
	t.Setenv("TZ", "")
	withDetectedHardware(t, testHardware)
	withZoneFiles(t, "America/Sao_Paulo")

	oldBase := telegramAPIBase
	t.Cleanup(func() { telegramAPIBase = oldBase })

	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"test_bot"}}`))
	}))
	t.Cleanup(tg.Close)
	telegramAPIBase = tg.URL

	return &wizardEnv{home: home, configPath: configPath, out: &bytes.Buffer{}}
}

// run feeds the lines to the wizard; hidden prompts read from the same input.
func (e *wizardEnv) run(t *testing.T, lines ...string) error {
	t.Helper()
	in := bufio.NewReader(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	return e.runWith(in, func() (string, error) {
		s, err := in.ReadString('\n')
		return strings.TrimRight(s, "\r\n"), err
	})
}

func (e *wizardEnv) runWith(in *bufio.Reader, secret func() (string, error)) error {
	return runWizard(wizardIO{in: in, out: e.out, readSecret: secret}, e.configPath, false, "", false)
}

func (e *wizardEnv) files(t *testing.T) (cfgJSON, security string) {
	t.Helper()
	c, err := os.ReadFile(e.configPath)
	require.NoError(t, err)
	s, err := os.ReadFile(filepath.Join(e.home, config.SecurityConfigFile))
	require.NoError(t, err)
	return string(c), string(s)
}

// modelsServer answers GET /models with two models and records the
// Authorization header it saw.
func modelsServer(t *testing.T, gotAuth *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			auth := "<absent>"
			if v, ok := r.Header["Authorization"]; ok {
				auth = strings.Join(v, ",")
			}
			*gotAuth = append(*gotAuth, auth)
		}
		assert.Equal(t, "/models", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func findModel(cfg *config.Config, alias string) *config.ModelConfig {
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == alias {
			return m
		}
	}
	return nil
}

func TestWizard_PtBRHappyPath(t *testing.T) {
	e := newWizardEnv(t)
	var auth []string
	srv := modelsServer(t, &auth)

	err := e.run(t,
		"1",       // idioma pt-BR
		"1",       // OpenAI-compatível
		srv.URL,   // base
		"sk-test", // chave
		"1",       // modelo model-a
		"1",       // Telegram
		testToken, // token
		"987654",  // ID do dono
		"",        // perfil detectado
		"",        // fuso padrão
		"2",       // Assistente
		"S",       // gravar
	)
	require.NoError(t, err, e.out.String())
	assert.Equal(t, []string{"Bearer sk-test"}, auth)

	cfg, err := config.LoadConfig(e.configPath)
	require.NoError(t, err)

	assert.Equal(t, "default", cfg.Agents.Defaults.GetModelName())
	m := findModel(cfg, "default")
	require.NotNil(t, m)
	assert.Equal(t, srv.URL, m.APIBase)
	assert.Equal(t, "model-a", m.Model)
	assert.Empty(t, m.Provider)
	assert.Equal(t, "sk-test", m.APIKey())

	tg := cfg.Channels[config.ChannelTelegram]
	require.NotNil(t, tg)
	assert.True(t, tg.Enabled)
	assert.Equal(t, []string{"987654"}, []string(tg.AllowFrom))
	decoded, err := tg.GetDecoded()
	require.NoError(t, err)
	assert.Equal(t, testToken, decoded.(*config.TelegramSettings).Token.String())

	cfgJSON, security := e.files(t)
	assert.NotContains(t, cfgJSON, "sk-test")
	assert.NotContains(t, cfgJSON, testToken)
	assert.Contains(t, security, "sk-test")
	assert.Contains(t, security, testToken)

	edit, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, hwprofile.Select(testHardware), hwprofile.Match(edit, testHardware))
	assert.Equal(t, "America/Sao_Paulo", cfg.Timezone)
	assert.Equal(t, setup.PresetAssistant, setup.MatchPermissions(edit))

	user, err := os.ReadFile(filepath.Join(cfg.WorkspacePath(), "USER.md"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(user), "- Idioma preferido: pt-BR"))

	out := e.out.String()
	assert.NotContains(t, out, "sk-test", "the summary shows the key masked")
	assert.NotContains(t, out, testToken)
	assert.Contains(t, out, "kuromatsu gateway")
}

func TestWizard_OllamaNoAuthHeader(t *testing.T) {
	e := newWizardEnv(t)
	var auth []string
	srv := modelsServer(t, &auth)

	err := e.run(t, "2", "2", srv.URL, "2", "2", "", "", "", "y")
	require.NoError(t, err, e.out.String())
	assert.Equal(t, []string{"<absent>"}, auth, "the Ollama probe must not send Authorization")

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	m := findModel(cfg, "default")
	require.NotNil(t, m)
	assert.Equal(t, "ollama", m.Provider)
	assert.Equal(t, "model-b", m.Model)
	assert.Empty(t, m.APIKeys)
	assert.Contains(t, e.out.String(), "kuromatsu agent")
}

func TestWizard_RejectsEmptyOwnerID(t *testing.T) {
	e := newWizardEnv(t)

	err := e.run(t, "2", "6", "1", testToken, "", "abc", "12a", "987654", "", "", "", "y")
	require.NoError(t, err, e.out.String())
	assert.Equal(t, 3, strings.Count(e.out.String(), "Digits only"))

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"987654"}, []string(cfg.Channels[config.ChannelTelegram].AllowFrom))
}

func TestWizard_QuitWritesNothing(t *testing.T) {
	t.Run("fresh home", func(t *testing.T) {
		e := newWizardEnv(t)
		err := e.run(t, "1", "6", "2", "q")
		require.ErrorIs(t, err, errWizardQuit)
		_, statErr := os.Stat(e.configPath)
		assert.True(t, os.IsNotExist(statErr), "config.json must not be created")
		_, statErr = os.Stat(filepath.Join(e.home, "workspace"))
		assert.True(t, os.IsNotExist(statErr), "workspace must not be created")
	})
	t.Run("existing config", func(t *testing.T) {
		e := newWizardEnv(t)
		require.NoError(t, config.SaveConfig(e.configPath, config.DefaultConfig()))
		old := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(e.configPath, old, old))
		before, err := os.ReadFile(e.configPath)
		require.NoError(t, err)

		err = e.run(t, "1", "6", "2", "q")
		require.ErrorIs(t, err, errWizardQuit)
		info, err := os.Stat(e.configPath)
		require.NoError(t, err)
		assert.True(t, info.ModTime().Equal(old), "mtime changed")
		after, err := os.ReadFile(e.configPath)
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after))
	})
	t.Run("declined summary", func(t *testing.T) {
		e := newWizardEnv(t)
		err := e.run(t, "2", "6", "2", "", "", "", "n")
		require.ErrorIs(t, err, errWizardQuit)
		_, statErr := os.Stat(e.configPath)
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestWizard_SaveAnywayOnProbeFailure(t *testing.T) {
	e := newWizardEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusBadGateway)
	}))
	defer srv.Close()

	err := e.run(t, "2", "1", srv.URL, "sk-manual-key", "r", "s", "my-model", "2", "", "", "", "y")
	require.NoError(t, err, e.out.String())
	out := e.out.String()
	assert.Equal(t, 2, strings.Count(out, "HTTP 502"), "probe then retry")

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	m := findModel(cfg, "default")
	require.NotNil(t, m)
	assert.Equal(t, "my-model", m.Model)
	assert.Equal(t, srv.URL, m.APIBase)
	assert.Equal(t, "default", cfg.Agents.Defaults.GetModelName())
}

// existingInstall writes a config with a model, a Telegram channel and the
// default permissions, as an earlier onboard would.
func existingInstall(t *testing.T, e *wizardEnv, modelsURL string) {
	t.Helper()
	cfg := config.DefaultConfig()
	setup.ApplyModelChoice(cfg, setup.ModelChoice{
		Alias: "mine", Provider: "openrouter", Model: "old-model", APIBase: modelsURL, APIKey: "sk-old-key-1234",
	})
	tg := cfg.Channels[config.ChannelTelegram]
	tg.Enabled = true
	tg.AllowFrom = config.FlexibleStringSlice{"555"}
	decoded, err := tg.GetDecoded()
	require.NoError(t, err)
	decoded.(*config.TelegramSettings).Token.Set(testToken)
	require.NoError(t, config.SaveConfig(e.configPath, cfg))
}

func TestWizard_RerunKeepsExisting(t *testing.T) {
	e := newWizardEnv(t)
	var auth []string
	srv := modelsServer(t, &auth)
	existingInstall(t, e, srv.URL)

	require.NoError(t, e.run(t, "", "", "", "", "", "", ""), e.out.String())
	assert.Empty(t, auth, "keeping the model makes no probe")

	cfg, err := config.LoadConfig(e.configPath)
	require.NoError(t, err)
	m := findModel(cfg, "mine")
	require.NotNil(t, m)
	assert.Equal(t, "old-model", m.Model)
	assert.Equal(t, "openrouter", m.Provider)
	assert.Equal(t, srv.URL, m.APIBase)
	assert.Equal(t, "sk-old-key-1234", m.APIKey())
	assert.Equal(t, "mine", cfg.Agents.Defaults.GetModelName())
	assert.Nil(t, findModel(cfg, "default"))

	tg := cfg.Channels[config.ChannelTelegram]
	assert.True(t, tg.Enabled)
	assert.Equal(t, []string{"555"}, []string(tg.AllowFrom))
	decoded, err := tg.GetDecoded()
	require.NoError(t, err)
	assert.Equal(t, testToken, decoded.(*config.TelegramSettings).Token.String())
	assert.Contains(t, e.out.String(), "[0] Manter atual (mine: old-model)")
}

// Choosing the same provider again on a rerun starts from the current base
// and key: Enter on both keeps them.
func TestWizard_RerunEditModelKeepsKey(t *testing.T) {
	e := newWizardEnv(t)
	var auth []string
	srv := modelsServer(t, &auth)
	existingInstall(t, e, srv.URL)

	require.NoError(t, e.run(t, "", "5", "", "", "2", "", "", "", "", ""), e.out.String())
	assert.Equal(t, []string{"Bearer sk-old-key-1234"}, auth)

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	m := findModel(cfg, "mine")
	require.NotNil(t, m)
	assert.Equal(t, "model-b", m.Model)
	assert.Equal(t, "sk-old-key-1234", m.APIKey())
}

func TestWizard_RerunDefaultsToKeepPermissions(t *testing.T) {
	e := newWizardEnv(t)
	existingInstall(t, e, modelsServer(t, nil).URL)
	want := setup.MatchPermissions(config.DefaultConfig())

	require.NoError(t, e.run(t, "", "", "", "", "", "", ""), e.out.String())
	assert.Contains(t, e.out.String(), "[0] Manter atual ("+string(want)+")")

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	def := config.DefaultConfig()
	assert.Equal(t, def.Tools.Exec.Enabled, cfg.Tools.Exec.Enabled)
	assert.Equal(t, def.Tools.Cron.AllowCommand, cfg.Tools.Cron.AllowCommand)
	assert.Equal(t, def.Tools.Exec.AllowRemote, cfg.Tools.Exec.AllowRemote)
	assert.Equal(t, def.Tools.Web.Enabled, cfg.Tools.Web.Enabled)
	assert.Equal(t, def.Tools.Cron.Enabled, cfg.Tools.Cron.Enabled)
	assert.Equal(t, def.Agents.Defaults.RestrictToWorkspace, cfg.Agents.Defaults.RestrictToWorkspace)
}

func TestWizard_ErrorsDoNotLeakToken(t *testing.T) {
	e := newWizardEnv(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	telegramAPIBase = dead.URL
	dead.Close() // connection refused: the *url.Error carries the full URL

	require.NoError(t, e.run(t, "2", "6", "1", testToken, "987654", "", "", "", "y"), e.out.String())
	out := e.out.String()
	assert.Contains(t, out, "could not verify the bot")
	assert.False(t, strings.Contains(out, testToken), "the token leaked: %s", out)
	assert.NotContains(t, out, strings.Repeat("A", 35))
}

func TestWizard_SecretFallbackVisible(t *testing.T) {
	e := newWizardEnv(t)
	srv := modelsServer(t, nil)
	in := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"2", "1", srv.URL, "sk-visible-key", "1", "2", "", "", "", "y",
	}, "\n") + "\n"))
	failing := func() (string, error) { return "", errors.New("inappropriate ioctl for device") }

	require.NoError(t, e.runWith(in, failing), e.out.String())
	assert.Contains(t, e.out.String(), "input will be visible")

	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	m := findModel(cfg, "default")
	require.NotNil(t, m)
	assert.Equal(t, "sk-visible-key", m.APIKey())
}

// Without a terminal on stdin, onboard asks nothing and writes the same
// config.json as --non-interactive.
func TestOnboard_NonTTYUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	configPath := filepath.Join(home, "config.json")
	t.Setenv(config.EnvConfig, configPath)
	withDetectedHardware(t, testHardware)

	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	_ = w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin })

	require.False(t, interactive(false, false), "a pipe on stdin is not a terminal")
	onboard(false, false, "", false)
	piped, err := os.ReadFile(configPath)
	require.NoError(t, err)

	require.NoError(t, os.Remove(configPath))
	onboard(false, false, "", true)
	flagged, err := os.ReadFile(configPath)
	require.NoError(t, err)

	assert.Equal(t, string(flagged), string(piped))
}

// With no zone known (no TZ, nothing detected), Enter at the timezone step
// leaves "timezone" empty, on a new config and on a rerun alike.
func TestWizard_TimezoneUndetectedStaysEmpty(t *testing.T) {
	e := newWizardEnv(t)
	withZoneFiles(t, "")

	require.NoError(t, e.run(t, "2", "6", "2", "", "", "", "y"), e.out.String())
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Empty(t, cfg.Timezone)
	assert.Contains(t, e.out.String(), "Enter uses the system zone")

	e.out.Reset()
	require.NoError(t, e.run(t, "2", "", "", "", "", "", ""), e.out.String())
	cfg, err = config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Empty(t, cfg.Timezone)
}

// A detected zone is the default and Enter writes it.
func TestWizard_TimezoneDetectedDefault(t *testing.T) {
	e := newWizardEnv(t)
	withZoneFiles(t, "Europe/Lisbon")

	require.NoError(t, e.run(t, "2", "6", "2", "", "", "", "y"), e.out.String())
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Equal(t, "Europe/Lisbon", cfg.Timezone)
}

// A rerun on a config without a remote default model (e.g. one written by
// --non-interactive) skips the provider step on Enter.
func TestWizard_RerunWithoutRemoteModelSkipsProvider(t *testing.T) {
	e := newWizardEnv(t)
	require.NoError(t, config.SaveConfig(e.configPath, config.DefaultConfig()))
	before, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)

	require.NoError(t, e.run(t, "", "", "", "", "", "", ""), e.out.String())
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	assert.Len(t, cfg.ModelList, len(before.ModelList))
	assert.Nil(t, findModel(cfg, "default"))
	assert.Equal(t, before.Agents.Defaults.GetModelName(), cfg.Agents.Defaults.GetModelName())
}

// Re-choosing the provider kind of the current entry keeps its provider
// value (an explicit "openai" is not rewritten to empty).
func TestWizard_RerunSameKindKeepsProvider(t *testing.T) {
	e := newWizardEnv(t)
	srv := modelsServer(t, nil)
	cfg := config.DefaultConfig()
	setup.ApplyModelChoice(cfg, setup.ModelChoice{
		Alias: "mine", Provider: "openai", Model: "old-model", APIBase: srv.URL, APIKey: "sk-old-key-1234",
	})
	require.NoError(t, config.SaveConfig(e.configPath, cfg))

	require.NoError(t, e.run(t, "", "1", "", "", "1", "", "", "", "", ""), e.out.String())
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	m := findModel(cfg, "mine")
	require.NotNil(t, m)
	assert.Equal(t, "openai", m.Provider)
	assert.Equal(t, "model-a", m.Model)
}

// "None" on a rerun leaves an enabled Telegram channel as it is, so the
// next command is still the gateway.
func TestWizard_RerunNoneKeepsChannel(t *testing.T) {
	e := newWizardEnv(t)
	existingInstall(t, e, modelsServer(t, nil).URL)

	require.NoError(t, e.run(t, "", "", "2", "", "", "", ""), e.out.String())
	cfg, err := config.LoadConfigForEdit(e.configPath)
	require.NoError(t, err)
	tg := cfg.Channels[config.ChannelTelegram]
	assert.True(t, tg.Enabled)
	assert.Equal(t, []string{"555"}, []string(tg.AllowFrom))
	assert.Contains(t, e.out.String(), "kuromatsu gateway")
}

// The input ending inside the model picker quits quietly, writing nothing.
func TestWizard_EOFInPickerQuits(t *testing.T) {
	e := newWizardEnv(t)
	srv := modelsServer(t, nil)

	err := e.run(t, "2", "1", srv.URL, "sk-test")
	require.ErrorIs(t, err, errWizardQuit)
	_, statErr := os.Stat(e.configPath)
	assert.True(t, os.IsNotExist(statErr))
}
