package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

const (
	testAPIKey   = "sk-test-secret-key-0123456789"
	testTGToken  = "123456:tg-secret-token-ABCDEFGHIJKLMNOPQRSTUV"
	testOwnerID  = "987654321"
	testOtherID  = "555000111"
	testPicoTokn = "pico-secret-token-xyz"
)

func telegramChannel(t *testing.T, allowFrom ...string) *config.Channel {
	t.Helper()
	tg := &config.Channel{Type: config.ChannelTelegram, Enabled: true, AllowFrom: allowFrom}
	tg.SetName(config.ChannelTelegram)
	tg.Settings = config.RawNode("{}")
	decoded, err := tg.GetDecoded()
	if err != nil {
		t.Fatal(err)
	}
	decoded.(*config.TelegramSettings).Token = *config.NewSecureString(testTGToken)
	return tg
}

// configRuntime wires the /config fields the way the agent loop does, over
// cfg, with SetConfigKey and ReloadConfig recorded.
type configRecorder struct {
	sets    []string
	reloads int
}

func configRuntime(cfg *config.Config, rec *configRecorder) *Runtime {
	return &Runtime{
		Config: cfg,
		ConfigSummary: func() (string, error) {
			return setup.Summary(cfg, "", ""), nil
		},
		SetConfigKey: func(key, value string) (string, string, error) {
			rec.sets = append(rec.sets, key+"="+value)
			return "30", value, nil
		},
		CanMutate: func(channel, senderID string) bool {
			return setup.CanMutate(cfg, channel, senderID)
		},
		ReloadConfig: func() error {
			rec.reloads++
			return nil
		},
	}
}

func runCommand(t *testing.T, rt *Runtime, channel, chatID, sender, text string) string {
	t.Helper()
	var reply string
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)
	res := ex.Execute(context.Background(), Request{
		Channel: channel, ChatID: chatID, SenderID: sender, Text: text,
		Reply: func(s string) error { reply = s; return nil },
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("%q not handled", text)
	}
	if res.Err != nil {
		t.Fatalf("%q: %v", text, res.Err)
	}
	return reply
}

func TestConfigCommand_SummaryHasNoSecrets(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ModelList = []*config.ModelConfig{{
		ModelName: "main", Model: "openai/gpt-4o-mini", APIKeys: config.SimpleSecureStrings(testAPIKey),
	}}
	cfg.Agents.Defaults.ModelName = "main"
	cfg.Agents.Defaults.Provider = "openai"
	pico := &config.Channel{Type: config.ChannelPico, Enabled: true, AllowFrom: config.FlexibleStringSlice{"*"}}
	pico.SetName(config.ChannelPico)
	pico.Settings = config.RawNode(`{"token":"` + testPicoTokn + `"}`)
	cfg.Channels = config.ChannelsConfig{
		config.ChannelTelegram: telegramChannel(t, testOwnerID),
		config.ChannelPico:     pico,
	}
	cfg.Heartbeat.Enabled, cfg.Heartbeat.Interval = true, 30
	cfg.Timezone = "America/Sao_Paulo"
	cfg.Tools.Web.Enabled, cfg.Tools.Web.DuckDuckGo.Enabled = true, true
	cfg.Agents.Defaults.UserTurnMaxMinutes = 30
	cfg.Gateway.LogLevel = "info"

	reply := runCommand(t, configRuntime(cfg, &configRecorder{}), "cli", "direct", "local", "/config")
	lines := strings.Split(reply, "\n")
	want := []string{
		"Model: main (openai)",
		"Channels: pico (open), telegram (allow_from: 1 ID)",
		"Permissions: ",
		"Heartbeat: on, every 30 min",
		"Timezone: America/Sao_Paulo",
		"Web search: duckduckgo on",
		"User turn limit: 30 min",
		"Log level: info",
	}
	if len(lines) != len(want) {
		t.Fatalf("summary has %d lines, want %d:\n%s", len(lines), len(want), reply)
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Fatalf("line %d = %q, want prefix %q", i, lines[i], w)
		}
	}
	for _, secret := range []string{testAPIKey, testTGToken, "tg-secret", testPicoTokn, testOwnerID} {
		if strings.Contains(reply, secret) {
			t.Fatalf("summary leaks %q:\n%s", secret, reply)
		}
	}
}

func TestConfigSet_OwnerRule(t *testing.T) {
	cases := []struct {
		name      string
		channel   string
		allowFrom []string
		sender    string
		saved     bool
	}{
		{"internal channel", "cli", nil, "anyone", true},
		{"open bot", "telegram", []string{"*"}, "123", false},
		{"bare ID", "telegram", []string{"123"}, "123", true},
		{"canonical ID", "telegram", []string{"telegram:123"}, "123", true},
		{"ID with username", "telegram", []string{"123|alice"}, "123", true},
		{"other platform", "telegram", []string{"whatsapp:123"}, "123", false},
		{"username with @", "telegram", []string{"@alice"}, "123|alice", false},
		{"bare username", "telegram", []string{"alice"}, "123|alice", false},
		{"other ID", "telegram", []string{"124"}, "123", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Channels = config.ChannelsConfig{config.ChannelTelegram: telegramChannel(t, tc.allowFrom...)}
			rec := &configRecorder{}
			reply := runCommand(
				t,
				configRuntime(cfg, rec),
				tc.channel,
				"42",
				tc.sender,
				"/config set heartbeat.interval 45",
			)
			if tc.saved {
				if len(rec.sets) != 1 || rec.sets[0] != "heartbeat.interval=45" || rec.reloads != 1 {
					t.Fatalf("sets=%v reloads=%d reply=%q, want one save and one reload", rec.sets, rec.reloads, reply)
				}
				if reply != "heartbeat.interval: 30 → 45 (reloading)" {
					t.Fatalf("reply = %q", reply)
				}
				return
			}
			if len(rec.sets) != 0 || rec.reloads != 0 {
				t.Fatalf("sets=%v reloads=%d, want nothing saved", rec.sets, rec.reloads)
			}
			if reply != ownerOnlyMsg {
				t.Fatalf("reply = %q, want the owner-only message", reply)
			}
		})
	}
}

// Through the real pipeline (setup.SaveKey on a config file): an invalid value
// or a key outside the whitelist writes nothing and triggers no reload.
func TestConfigSet_ValidationAndWhitelist(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	t.Setenv("KUROMATSU_KEY_PASSPHRASE", "")
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultConfig()
	cfg.Heartbeat.Interval = 30
	if err := config.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	rec := &configRecorder{}
	rt := configRuntime(cfg, rec)
	rt.SetConfigKey = func(key, value string) (string, string, error) {
		return setup.SaveKey(path, key, value)
	}

	reply := runCommand(t, rt, "cli", "direct", "local", "/config set heartbeat.interval 3")
	if !strings.Contains(reply, "minimum 5") {
		t.Fatalf("reply = %q, want the minimum", reply)
	}
	reply = runCommand(t, rt, "cli", "direct", "local", "/config set tools.exec.allow_remote true")
	if !strings.Contains(reply, "not settable from chat") {
		t.Fatalf("reply = %q, want not settable", reply)
	}
	reply = runCommand(t, rt, "cli", "direct", "local", "/config set heartbeat.interval")
	if !strings.Contains(reply, "Usage") {
		t.Fatalf("reply = %q, want usage", reply)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) || rec.reloads != 0 {
		t.Fatalf("config changed or reloaded (reloads=%d)", rec.reloads)
	}

	reply = runCommand(t, rt, "cli", "direct", "local", `/config set timezone "America/Sao_Paulo"`)
	if !strings.HasPrefix(reply, `timezone: "" → America/Sao_Paulo (reloading`) || rec.reloads != 1 {
		t.Fatalf("reply = %q reloads=%d", reply, rec.reloads)
	}
	got, err := config.LoadConfigForEdit(path)
	if err != nil || got.Timezone != "America/Sao_Paulo" {
		t.Fatalf("saved timezone = %q, %v", got.Timezone, err)
	}
}

func TestConfigSummary_OpenBotOwnerOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels = config.ChannelsConfig{config.ChannelTelegram: telegramChannel(t, "*")}
	rt := configRuntime(cfg, &configRecorder{})
	if reply := runCommand(t, rt, "telegram", "42", "123", "/config"); reply != ownerOnlyMsg {
		t.Fatalf("open bot, stranger: reply = %q", reply)
	}

	cfg.Channels = config.ChannelsConfig{config.ChannelTelegram: telegramChannel(t, "*", "123")}
	if reply := runCommand(t, rt, "telegram", "42", "123", "/config"); !strings.HasPrefix(reply, "Model: ") {
		t.Fatalf("open bot, owner: reply = %q", reply)
	}

	// A channel that is not open shows the summary to any accepted sender.
	cfg.Channels = config.ChannelsConfig{config.ChannelTelegram: telegramChannel(t, "123", "456")}
	if reply := runCommand(t, rt, "telegram", "42", "456", "/config"); !strings.HasPrefix(reply, "Model: ") {
		t.Fatalf("closed bot, listed sender: reply = %q", reply)
	}

	if reply := runCommand(t, &Runtime{}, "cli", "direct", "local", "/config"); reply != unavailableMsg {
		t.Fatalf("no runtime fields: reply = %q", reply)
	}
}

func TestSplitArgs(t *testing.T) {
	got, err := splitArgs(`/cron new lembrete texto="beber água" quando=“10:00” x=""`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/cron", "new", "lembrete", "texto=beber água", "quando=10:00", "x="}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("splitArgs = %q, want %q", got, want)
	}
	if got, _ := splitArgs(`/config set timezone ""`); len(got) != 4 || got[3] != "" {
		t.Fatalf("empty quoted argument lost: %q", got)
	}
	if _, err := splitArgs(`a "b`); err == nil {
		t.Fatal("unterminated quote accepted")
	}
}

// A bus channel name with no config entry of that name fails closed.
func TestConfigSummary_UnknownChannelFailsClosed(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels = config.ChannelsConfig{"my_whatsapp": &config.Channel{
		Type: "whatsapp", Enabled: true, AllowFrom: config.FlexibleStringSlice{"*"},
	}}
	rt := configRuntime(cfg, &configRecorder{})
	if reply := runCommand(
		t,
		rt,
		"whatsapp",
		"5511@s.whatsapp.net",
		"5511@s.whatsapp.net",
		"/config",
	); reply != ownerOnlyMsg {
		t.Fatalf("unresolved channel: reply = %q", reply)
	}
}

// Without a reloader (kuromatsu agent) the change is saved and applies on the
// next start; the reply does not report a failed reload.
func TestConfigSet_NoReloaderSavesForNextStart(t *testing.T) {
	rec := &configRecorder{}
	rt := configRuntime(config.DefaultConfig(), rec)
	rt.ReloadConfig = func() error { return ErrReloadUnavailable }
	reply := runCommand(t, rt, "cli", "direct", "local", "/config set heartbeat.interval 45")
	if reply != "heartbeat.interval: 30 → 45 (saved; applies on the next start)" || len(rec.sets) != 1 {
		t.Fatalf("reply = %q sets=%v", reply, rec.sets)
	}
	rt.ReloadConfig = func() error { return errors.New("boom") }
	if reply := runCommand(
		t,
		rt,
		"cli",
		"direct",
		"local",
		"/config set heartbeat.interval 45",
	); !strings.Contains(
		reply,
		"reload failed: boom",
	) {
		t.Fatalf("real reload failure: reply = %q", reply)
	}
}
