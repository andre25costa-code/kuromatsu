package openclaw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestLoadOpenClawConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")

	testConfig := `{
		"agents": {
			"defaults": {
				"model": {
					"primary": "anthropic/claude-sonnet-4-20250514"
				},
				"workspace": "~/.openclaw/workspace"
			},
			"list": [
				{
					"id": "main",
					"name": "Main Agent",
					"model": {
						"primary": "openai/gpt-4o",
						"fallbacks": ["claude-3-opus"]
					}
				}
			]
		},
		"channels": {
			"telegram": {
				"enabled": true,
				"botToken": "test-token",
				"allowFrom": ["user1", "user2"]
			},
			"discord": {
				"enabled": true,
				"token": "discord-token"
			}
		},
		"models": {
			"providers": {
				"anthropic": {
					"api_key": "sk-ant-test",
					"base_url": "https://api.anthropic.com"
				},
				"openai": {
					"api_key": "sk-test"
				}
			}
		}
	}`

	err := os.WriteFile(configPath, []byte(testConfig), 0o644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadOpenClawConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Agents == nil {
		t.Error("agents should not be nil")
	}

	if cfg.Agents.Defaults == nil {
		t.Error("agents.defaults should not be nil")
	}

	provider, model := cfg.GetDefaultModel()
	if provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got '%s'", provider)
	}
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got '%s'", model)
	}

	workspace := cfg.GetDefaultWorkspace()
	if workspace != "~/.kuromatsu/workspace" {
		t.Errorf("expected workspace '~/.kuromatsu/workspace', got '%s'", workspace)
	}

	agents := cfg.GetAgents()
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].ID != "main" {
		t.Errorf("expected agent id 'main', got '%s'", agents[0].ID)
	}

	if cfg.Channels == nil {
		t.Error("channels should not be nil")
	}
	if cfg.Channels.Telegram == nil {
		t.Error("telegram channel should not be nil")
	}
	if cfg.Channels.Telegram.BotToken == nil || *cfg.Channels.Telegram.BotToken != "test-token" {
		t.Error("telegram bot token not parsed correctly")
	}
}

func TestConvertToKuromatsu(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "openclaw.json")

	testConfig := `{
		"agents": {
			"defaults": {
				"model": {
					"primary": "anthropic/claude-sonnet-4-20250514"
				},
				"workspace": "~/.openclaw/workspace"
			},
			"list": [
				{
					"id": "main",
					"name": "Main Agent"
				},
				{
					"id": "assistant",
					"name": "Assistant",
					"skills": ["skill1", "skill2"]
				}
			]
		},
		"channels": {
			"telegram": {
				"enabled": true,
				"botToken": "test-token",
				"allowFrom": ["user1", "user2"]
			},
			"discord": {
				"enabled": false,
				"token": "discord-token"
			},
			"whatsapp": {
				"enabled": true,
				"bridgeUrl": "http://localhost:3000"
			},
			"feishu": {
				"enabled": true,
				"appId": "app-id",
				"appSecret": "app-secret",
				"allowFrom": ["user3"]
			},
			"signal": {
				"enabled": true
			}
		},
		"models": {
			"providers": {
				"anthropic": {
					"api_key": "sk-ant-test"
				},
				"openai": {
					"api_key": "sk-test"
				}
			}
		},
		"skills": {
			"entries": {
				"skill1": {}
			}
		},
		"memory": {"enabled": true},
		"cron": {"enabled": true}
	}`

	err := os.WriteFile(configPath, []byte(testConfig), 0o644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadOpenClawConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	picoCfg, warnings, err := cfg.ConvertToKuromatsu("")
	if err != nil {
		t.Fatalf("failed to convert config: %v", err)
	}

	if picoCfg.Agents.Defaults.ModelName != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got '%s'", picoCfg.Agents.Defaults.ModelName)
	}
	if picoCfg.Agents.Defaults.Workspace != "~/.kuromatsu/workspace" {
		t.Errorf("expected workspace '~/.kuromatsu/workspace', got '%s'", picoCfg.Agents.Defaults.Workspace)
	}

	if len(picoCfg.Agents.List) != 2 {
		t.Errorf("expected 2 agents, got %d", len(picoCfg.Agents.List))
	}
	if picoCfg.Agents.List[0].ID != "main" {
		t.Errorf("expected first agent id 'main', got '%s'", picoCfg.Agents.List[0].ID)
	}
	if picoCfg.Agents.List[1].Skills == nil || len(picoCfg.Agents.List[1].Skills) != 2 {
		t.Errorf("expected 2 skills for assistant agent")
	}

	if !picoCfg.Channels.Telegram.Enabled {
		t.Error("telegram should be enabled")
	}
	if picoCfg.Channels.Telegram.Token != "test-token" {
		t.Errorf("expected telegram token 'test-token', got '%s'", picoCfg.Channels.Telegram.Token)
	}

	if picoCfg.Channels.WhatsApp.BridgeURL != "http://localhost:3000" {
		t.Errorf("expected whatsapp bridge URL 'http://localhost:3000', got '%s'", picoCfg.Channels.WhatsApp.BridgeURL)
	}
	if picoCfg.Channels.Telegram == nil || picoCfg.Channels.WhatsApp == nil {
		t.Fatal("telegram and whatsapp are present in the source and must be converted")
	}
	if !warningsContain(warnings, "Channel 'feishu': not supported by Kuromatsu; ignored") {
		t.Errorf("expected feishu to be reported as ignored, warnings=%v", warnings)
	}

	if len(picoCfg.ModelList) != 1 {
		t.Errorf("expected 1 model config (no models.json provided), got %d", len(picoCfg.ModelList))
	}

	foundWarning := false
	for _, w := range warnings {
		if len(w) > 0 {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Log("warnings should be generated for skills, memory, cron, and unsupported channels")
	}
}

func TestToStandardConfig_ExecAllowRemoteDefaultsFalse(t *testing.T) {
	cfg := (&KuromatsuConfig{
		Tools: ToolsConfig{
			Exec: ExecConfig{
				EnableDenyPatterns: true,
			},
		},
	}).ToStandardConfig()

	if cfg.Tools.Exec.AllowRemote {
		t.Fatal("ToStandardConfig() should preserve the default tools.exec.allow_remote=false")
	}
}

func warningsContain(warnings []string, want string) bool {
	for _, w := range warnings {
		if w == want {
			return true
		}
	}
	return false
}

func convertFromJSON(t *testing.T, src string) (KuromatsuConfig, []string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(configPath, []byte(src), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	cfg, err := LoadOpenClawConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	out, warnings, err := cfg.ConvertToKuromatsu("")
	if err != nil {
		t.Fatalf("failed to convert config: %v", err)
	}
	return *out, warnings
}

func TestConvertChannels_OnlySupportedAndPresent(t *testing.T) {
	out, warnings := convertFromJSON(t, `{
		"channels": {
			"telegram": {"botToken": "t", "allowFrom": ["123"]},
			"discord": {"enabled": true, "token": "d"},
			"matrix": {"enabled": true, "homeserver": "https://m.example"},
			"qq": {"enabled": true},
			"dingtalk": {"enabled": true},
			"maixcam": {"enabled": true},
			"slack": {"enabled": true},
			"feishu": {"enabled": true}
		}
	}`)

	if out.Channels.Telegram == nil {
		t.Fatal("telegram must be converted")
	}
	if out.Channels.WhatsApp != nil {
		t.Fatal("whatsapp is absent from the source and must not be emitted")
	}
	for _, name := range []string{"discord", "matrix", "qq", "dingtalk", "maixcam", "slack", "feishu"} {
		want := "Channel '" + name + "': not supported by Kuromatsu; ignored"
		if !warningsContain(warnings, want) {
			t.Errorf("missing warning %q in %v", want, warnings)
		}
	}
	std := out.Channels.ToStandardChannels()
	if len(std) != 1 || std["telegram"] == nil {
		t.Fatalf("expected only telegram, got %v", std)
	}
}

func TestConvertChannels_EmptyAllowFromWarns(t *testing.T) {
	out, warnings := convertFromJSON(t, `{
		"channels": {
			"whatsapp": {"enabled": true, "bridgeUrl": "http://localhost:3000"},
			"telegram": {"botToken": "t", "allowFrom": ["1"]}
		}
	}`)

	if out.Channels.WhatsApp == nil {
		t.Fatal("whatsapp must be emitted")
	}
	want := "Channel 'whatsapp': allow_from is empty; it will reject all senders until you set it"
	if !warningsContain(warnings, want) {
		t.Errorf("missing warning %q in %v", want, warnings)
	}
	if warningsContain(
		warnings,
		"Channel 'telegram': allow_from is empty; it will reject all senders until you set it",
	) {
		t.Error("telegram has allow_from and must not warn")
	}
}

func TestToStandardChannels_CarriesAllowFrom(t *testing.T) {
	out, _ := convertFromJSON(t, `{
		"channels": {
			"telegram": {"botToken": "tok", "allowFrom": ["123", "456"], "useMarkdownV2": true},
			"whatsapp": {"enabled": true, "bridgeUrl": "ws://bridge", "allowFrom": ["+1555"]}
		}
	}`)
	std := out.Channels.ToStandardChannels()

	tg := std["telegram"]
	if tg == nil {
		t.Fatal("telegram missing")
	}
	if got := []string(tg.AllowFrom); len(got) != 2 || got[0] != "123" || got[1] != "456" {
		t.Errorf("telegram allow_from = %v", got)
	}
	if !tg.Enabled {
		t.Error("telegram must be enabled")
	}
	data, err := json.Marshal(tg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"use_markdown_v2":true`) {
		t.Errorf("use_markdown_v2 was not carried: %s", data)
	}

	wa := std["whatsapp"]
	if wa == nil {
		t.Fatal("whatsapp missing")
	}
	if got := []string(wa.AllowFrom); len(got) != 1 || got[0] != "+1555" {
		t.Errorf("whatsapp allow_from = %v", got)
	}
	data, _ = json.Marshal(wa)
	if !strings.Contains(string(data), `"bridge_url":"ws://bridge"`) {
		t.Errorf("bridge_url was not carried: %s", data)
	}
}

func TestConvertToKuromatsu_NoChannelsEmitsNone(t *testing.T) {
	out, _ := convertFromJSON(t, `{"channels": {}}`)
	if std := out.Channels.ToStandardChannels(); len(std) != 0 {
		t.Fatalf("expected no channels, got %v", std)
	}
}

func TestConvertToKuromatsuWithUnsupportedChannelsOnly(t *testing.T) {
	out, warnings := convertFromJSON(t, `{
		"channels": {
			"qq": {"enabled": true, "appId": "a", "appSecret": "s"},
			"matrix": {"enabled": false, "homeserver": "https://m.example"}
		}
	}`)
	if out.Channels.Telegram != nil || out.Channels.WhatsApp != nil {
		t.Fatal("nothing must be emitted")
	}
	for _, name := range []string{"qq", "matrix"} {
		if !warningsContain(warnings, "Channel '"+name+"': not supported by Kuromatsu; ignored") {
			t.Errorf("missing ignored warning for %s: %v", name, warnings)
		}
	}
}

func TestConvertChannels_WarnsForEveryUnknownChannelKey(t *testing.T) {
	out, warnings := convertFromJSON(t, `{
		"channels": {
			"telegram": {"botToken": "t", "allowFrom": ["123"]},
			"onebot": {"enabled": true},
			"line": {"enabled": true},
			"foo": {"anything": 1},
			"discord": {"enabled": true, "token": "d"},
			"slack": null
		}
	}`)

	var ignored []string
	for _, w := range warnings {
		if strings.HasSuffix(w, ": not supported by Kuromatsu; ignored") {
			ignored = append(ignored, w)
		}
	}
	want := []string{
		"Channel 'discord': not supported by Kuromatsu; ignored",
		"Channel 'foo': not supported by Kuromatsu; ignored",
		"Channel 'line': not supported by Kuromatsu; ignored",
		"Channel 'onebot': not supported by Kuromatsu; ignored",
	}
	if !slices.Equal(ignored, want) {
		t.Fatalf("ignored-channel warnings = %q, want %q", ignored, want)
	}

	std := out.Channels.ToStandardChannels()
	if len(std) != 1 || std["telegram"] == nil {
		t.Fatalf("expected only telegram, got %v", std)
	}
}

func TestOpenClawAgentModel(t *testing.T) {
	model := &OpenClawAgentModel{
		Primary:   strPtr("anthropic/claude-3-opus"),
		Fallbacks: []string{"claude-3-sonnet", "claude-3-haiku"},
	}

	primary := model.GetPrimary()
	if primary != "anthropic/claude-3-opus" {
		t.Errorf("expected primary 'anthropic/claude-3-opus', got '%s'", primary)
	}

	fallbacks := model.GetFallbacks()
	if len(fallbacks) != 2 {
		t.Errorf("expected 2 fallbacks, got %d", len(fallbacks))
	}

	model2 := &OpenClawAgentModel{
		Simple: "claude-3-opus",
	}

	primary2 := model2.GetPrimary()
	if primary2 != "claude-3-opus" {
		t.Errorf("expected primary 'claude-3-opus' from Simple, got '%s'", primary2)
	}
}

func TestGetDefaultModel(t *testing.T) {
	cfg := &OpenClawConfig{
		Agents: &OpenClawAgents{
			Defaults: &OpenClawAgentDefaults{
				Model: &OpenClawAgentModel{
					Primary: strPtr("openai/gpt-4"),
				},
			},
		},
	}

	provider, model := cfg.GetDefaultModel()
	if provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", provider)
	}
	if model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", model)
	}
}

func TestGetDefaultModelWithNoDefaults(t *testing.T) {
	cfg := &OpenClawConfig{}

	provider, model := cfg.GetDefaultModel()
	if provider != "anthropic" {
		t.Errorf("expected default provider 'anthropic', got '%s'", provider)
	}
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("expected default model 'claude-sonnet-4-20250514', got '%s'", model)
	}
}

func TestHasFunctions(t *testing.T) {
	cfg := &OpenClawConfig{
		Skills:  &OpenClawSkills{Entries: map[string]json.RawMessage{"skill1": nil}},
		Memory:  json.RawMessage(`{"enabled": true}`),
		Cron:    json.RawMessage(`{"enabled": true}`),
		Hooks:   json.RawMessage(`{"enabled": true}`),
		Session: json.RawMessage(`{"enabled": true}`),
		Auth:    &OpenClawAuth{Profiles: json.RawMessage(`{"profile1": {}}`)},
	}

	if !cfg.HasSkills() {
		t.Error("should have skills")
	}
	if !cfg.HasMemory() {
		t.Error("should have memory")
	}
	if !cfg.HasCron() {
		t.Error("should have cron")
	}
	if !cfg.HasHooks() {
		t.Error("should have hooks")
	}
	if !cfg.HasSession() {
		t.Error("should have session")
	}
	if !cfg.HasAuthProfiles() {
		t.Error("should have auth profiles")
	}

	cfg2 := &OpenClawConfig{}
	if cfg2.HasSkills() {
		t.Error("should not have skills")
	}
	if cfg2.HasMemory() {
		t.Error("should not have memory")
	}
}

func TestToStandardConfig(t *testing.T) {
	picoCfg := &KuromatsuConfig{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Provider:  "anthropic",
				ModelName: "claude-sonnet-4-20250514",
				Workspace: "~/.kuromatsu/workspace",
			},
			List: []AgentConfig{
				{
					ID:      "main",
					Name:    "Main Agent",
					Default: true,
				},
			},
		},
		ModelList: []ModelConfig{
			{
				ModelName: "claude-sonnet-4-20250514",
				Model:     "anthropic/claude-sonnet-4-20250514",
				APIKey:    "sk-ant-test",
			},
		},
		Channels: ChannelsConfig{
			Telegram: &TelegramConfig{
				Enabled:   true,
				Token:     "test-token",
				AllowFrom: []string{"user1"},
			},
			WhatsApp: &WhatsAppConfig{
				Enabled:   true,
				BridgeURL: "http://localhost:3000",
			},
		},
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
	}

	stdCfg := picoCfg.ToStandardConfig()

	if stdCfg.Agents.Defaults.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got '%s'", stdCfg.Agents.Defaults.Provider)
	}
	if stdCfg.Agents.Defaults.ModelName != "claude-sonnet-4-20250514" {
		t.Errorf("expected model name 'claude-sonnet-4-20250514', got '%s'", stdCfg.Agents.Defaults.ModelName)
	}
	if stdCfg.Agents.Defaults.Workspace != "~/.kuromatsu/workspace" {
		t.Errorf("expected workspace '~/.kuromatsu/workspace', got '%s'", stdCfg.Agents.Defaults.Workspace)
	}

	if len(stdCfg.Agents.List) != 1 {
		t.Errorf("expected 1 agent, got %d", len(stdCfg.Agents.List))
	}
	if stdCfg.Agents.List[0].ID != "main" {
		t.Errorf("expected agent id 'main', got '%s'", stdCfg.Agents.List[0].ID)
	}

	foundModel := false
	var foundAPIKey string
	for _, m := range stdCfg.ModelList {
		if m.ModelName == "claude-sonnet-4-20250514" {
			foundModel = true
			foundAPIKey = m.APIKey()
			break
		}
	}
	if !foundModel {
		t.Error("expected to find claude-sonnet-4-20250514 model config")
	}
	if foundAPIKey != "sk-ant-test" {
		t.Errorf("expected api key 'sk-ant-test', got '%s'", foundAPIKey)
	}

	if !stdCfg.Channels["telegram"].Enabled {
		t.Error("telegram should be enabled")
	}
	decoded, err := stdCfg.Channels["telegram"].GetDecoded()
	if err != nil {
		t.Fatalf("GetDecoded() error = %v", err)
	}
	if tCfg, ok := decoded.(*config.TelegramSettings); ok &&
		tCfg.Token.String() != "test-token" {
		t.Errorf("expected token 'test-token', got '%s'", tCfg.Token.String())
	}

	if stdCfg.Gateway.Port != 8080 {
		t.Errorf("expected gateway port 8080, got %d", stdCfg.Gateway.Port)
	}
}

func TestLoadProviderConfigFromAgentsDir(t *testing.T) {
	tmpDir := t.TempDir()

	agentsDir := filepath.Join(tmpDir, "agents", "main", "agent")
	err := os.MkdirAll(agentsDir, 0o755)
	if err != nil {
		t.Fatalf("failed to create agents dir: %v", err)
	}

	modelsJSON := `{
		"providers": {
			"anthropic": {
				"baseUrl": "https://api.anthropic.com",
				"api": "anthropic",
				"apiKey": "sk-ant-from-models",
				"models": [
					{
						"id": "claude-sonnet-4-20250514",
						"name": "Claude Sonnet 4"
					}
				]
			},
			"openai": {
				"baseUrl": "https://api.openai.com",
				"api": "openai",
				"apiKey": "sk-from-models",
				"models": [
					{
						"id": "gpt-4o",
						"name": "GPT-4o"
					}
				]
			},
			"zhipu": {
				"baseUrl": "https://open.bigmodel.cn/api/paas/v4",
				"api": "openai",
				"apiKey": "zhipu-key",
				"models": []
			}
		}
	}`

	err = os.WriteFile(filepath.Join(agentsDir, "models.json"), []byte(modelsJSON), 0o644)
	if err != nil {
		t.Fatalf("failed to write models.json: %v", err)
	}

	providers := GetProviderConfigFromDir(tmpDir)
	if len(providers) != 3 {
		t.Errorf("expected 3 providers, got %d", len(providers))
	}

	if anthropic, ok := providers["anthropic"]; ok {
		if anthropic.ApiKey != "sk-ant-from-models" {
			t.Errorf("expected anthropic apiKey 'sk-ant-from-models', got '%s'", anthropic.ApiKey)
		}
		if anthropic.BaseUrl != "https://api.anthropic.com" {
			t.Errorf("expected anthropic baseUrl 'https://api.anthropic.com', got '%s'", anthropic.BaseUrl)
		}
	} else {
		t.Error("anthropic provider not found")
	}

	if openai, ok := providers["openai"]; ok {
		if openai.ApiKey != "sk-from-models" {
			t.Errorf("expected openai apiKey 'sk-from-models', got '%s'", openai.ApiKey)
		}
		if openai.BaseUrl != "https://api.openai.com" {
			t.Errorf("expected openai baseUrl 'https://api.openai.com', got '%s'", openai.BaseUrl)
		}
	} else {
		t.Error("openai provider not found")
	}

	if zhipu, ok := providers["zhipu"]; ok {
		if zhipu.ApiKey != "zhipu-key" {
			t.Errorf("expected zhipu apiKey 'zhipu-key', got '%s'", zhipu.ApiKey)
		}
		if zhipu.BaseUrl != "https://open.bigmodel.cn/api/paas/v4" {
			t.Errorf("expected zhipu baseUrl 'https://open.bigmodel.cn/api/paas/v4', got '%s'", zhipu.BaseUrl)
		}
	} else {
		t.Error("zhipu provider not found")
	}
}

func TestGetProviderConfigFromDirNotExist(t *testing.T) {
	providers := GetProviderConfigFromDir("/nonexistent/path")
	if len(providers) != 0 {
		t.Errorf("expected 0 providers for nonexistent path, got %d", len(providers))
	}
}

func strPtr(s string) *string {
	return &s
}
