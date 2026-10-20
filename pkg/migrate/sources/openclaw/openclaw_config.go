package openclaw

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

type OpenClawConfig struct {
	Auth     *OpenClawAuth     `json:"auth"`
	Models   *OpenClawModels   `json:"models"`
	Agents   *OpenClawAgents   `json:"agents"`
	Tools    *OpenClawTools    `json:"tools"`
	Channels *OpenClawChannels `json:"channels"`
	Cron     json.RawMessage   `json:"cron"`
	Hooks    json.RawMessage   `json:"hooks"`
	Skills   *OpenClawSkills   `json:"skills"`
	Memory   json.RawMessage   `json:"memory"`
	Session  json.RawMessage   `json:"session"`
}

type OpenClawAuth struct {
	Profiles json.RawMessage `json:"profiles"`
	Order    json.RawMessage `json:"order"`
}

type OpenClawModels struct {
	Providers map[string]json.RawMessage `json:"providers"`
}

type ProviderConfig struct {
	BaseUrl string        `json:"baseUrl"`
	Api     string        `json:"api"`
	Models  []ModelConfig `json:"models"`
	ApiKey  string        `json:"apiKey"`
}

type OpenClawModelConfig struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Reasoning     bool     `json:"reasoning"`
	Input         []string `json:"input"`
	Cost          Cost     `json:"cost"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
	Api           string   `json:"api,omitempty"`
}

type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

type OpenClawTools struct {
	Profile *string  `json:"profile"`
	Allow   []string `json:"allow"`
	Deny    []string `json:"deny"`
}

type OpenClawAgents struct {
	Defaults *OpenClawAgentDefaults `json:"defaults"`
	List     []OpenClawAgentEntry   `json:"list"`
}

type OpenClawAgentDefaults struct {
	Model     *OpenClawAgentModel `json:"model"`
	Workspace *string             `json:"workspace"`
	Tools     *OpenClawAgentTools `json:"tools"`
	Identity  *string             `json:"identity"`
}

type OpenClawAgentModel struct {
	Simple    string   `json:"-"`
	Primary   *string  `json:"primary"`
	Fallbacks []string `json:"fallbacks"`
}

func (m *OpenClawAgentModel) GetPrimary() string {
	if m.Simple != "" {
		return m.Simple
	}
	if m.Primary != nil {
		return *m.Primary
	}
	return ""
}

func (m *OpenClawAgentModel) GetFallbacks() []string {
	return m.Fallbacks
}

type OpenClawAgentEntry struct {
	ID        string              `json:"id"`
	Name      *string             `json:"name"`
	Model     *OpenClawAgentModel `json:"model"`
	Tools     *OpenClawAgentTools `json:"tools"`
	Workspace *string             `json:"workspace"`
	Skills    []string            `json:"skills"`
	Identity  *string             `json:"identity"`
}

type OpenClawAgentTools struct {
	Profile   *string  `json:"profile"`
	Allow     []string `json:"allow"`
	Deny      []string `json:"deny"`
	AlsoAllow []string `json:"alsoAllow"`
}

// OpenClawChannels keeps the two channels Kuromatsu can run. Every other key
// under "channels" is kept raw in Others so the migration can report it as
// ignored without describing each source channel.
type OpenClawChannels struct {
	Telegram *OpenClawTelegramConfig    `json:"telegram"`
	WhatsApp *OpenClawWhatsAppConfig    `json:"whatsapp"`
	Others   map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes telegram and whatsapp and keeps the remaining keys raw.
func (c *OpenClawChannels) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = OpenClawChannels{}
	if v, ok := raw["telegram"]; ok {
		if err := json.Unmarshal(v, &c.Telegram); err != nil {
			return fmt.Errorf("channels.telegram: %w", err)
		}
		delete(raw, "telegram")
	}
	if v, ok := raw["whatsapp"]; ok {
		if err := json.Unmarshal(v, &c.WhatsApp); err != nil {
			return fmt.Errorf("channels.whatsapp: %w", err)
		}
		delete(raw, "whatsapp")
	}
	if len(raw) > 0 {
		c.Others = raw
	}
	return nil
}

type OpenClawTelegramConfig struct {
	BotToken      *string  `json:"botToken"`
	AllowFrom     []string `json:"allowFrom"`
	GroupPolicy   *string  `json:"groupPolicy"`
	DmPolicy      *string  `json:"dmPolicy"`
	Enabled       *bool    `json:"enabled"`
	UseMarkdownV2 *bool    `json:"useMarkdownV2"`
}

type OpenClawWhatsAppConfig struct {
	AuthDir     *string  `json:"authDir"`
	DmPolicy    *string  `json:"dmPolicy"`
	AllowFrom   []string `json:"allowFrom"`
	GroupPolicy *string  `json:"groupPolicy"`
	Enabled     *bool    `json:"enabled"`
	BridgeURL   *string  `json:"bridgeUrl"`
}

type OpenClawSkills struct {
	Entries map[string]json.RawMessage `json:"entries"`
	Load    json.RawMessage            `json:"load"`
}

type OpenClawProviderConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

func (c *OpenClawConfig) GetEnabled() bool {
	return true
}

func LoadOpenClawConfig(path string) (*OpenClawConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config OpenClawConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &config, nil
}

func GetProviderConfigFromDir(dir string) map[string]ProviderConfig {
	result := make(map[string]ProviderConfig)
	p := filepath.Join(dir, "agents", "main", "agent", "models.json")

	if _, err := os.Stat(p); err != nil {
		return result
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return result
	}
	var models OpenClawModels
	if err := json.Unmarshal(data, &models); err != nil {
		return result
	}

	for name, raw := range models.Providers {
		var prov ProviderConfig
		if err := json.Unmarshal(raw, &prov); err != nil {
			continue
		}
		mappedName := mapProvider(name)
		result[mappedName] = prov
	}
	return result
}

func (c *OpenClawConfig) GetDefaultModel() (provider, model string) {
	if c.Agents == nil || c.Agents.Defaults == nil || c.Agents.Defaults.Model == nil {
		return "anthropic", "claude-sonnet-4-20250514"
	}

	primary := c.Agents.Defaults.Model.GetPrimary()
	if primary == "" {
		return "anthropic", "claude-sonnet-4-20250514"
	}

	parts := strings.Split(primary, "/")
	if len(parts) > 1 {
		return mapProvider(parts[0]), parts[1]
	}

	return "anthropic", primary
}

func (c *OpenClawConfig) GetDefaultWorkspace() string {
	if c.Agents == nil || c.Agents.Defaults == nil || c.Agents.Defaults.Workspace == nil {
		return ""
	}
	return rewriteWorkspacePath(*c.Agents.Defaults.Workspace)
}

func (c *OpenClawConfig) GetAgents() []OpenClawAgentEntry {
	if c.Agents == nil {
		return nil
	}
	return c.Agents.List
}

func (c *OpenClawConfig) HasSkills() bool {
	return c.Skills != nil && c.Skills.Entries != nil && len(c.Skills.Entries) > 0
}

func (c *OpenClawConfig) HasMemory() bool {
	return c.Memory != nil && len(c.Memory) > 0
}

func (c *OpenClawConfig) HasCron() bool {
	return c.Cron != nil && len(c.Cron) > 0
}

func (c *OpenClawConfig) HasHooks() bool {
	return c.Hooks != nil && len(c.Hooks) > 0
}

func (c *OpenClawConfig) HasSession() bool {
	return c.Session != nil && len(c.Session) > 0
}

func (c *OpenClawConfig) HasAuthProfiles() bool {
	return c.Auth != nil && c.Auth.Profiles != nil && len(c.Auth.Profiles) > 0
}

func (c *OpenClawConfig) ConvertToKuromatsu(sourceHome string) (*KuromatsuConfig, []string, error) {
	cfg := &KuromatsuConfig{}
	var warnings []string

	provider, modelName := c.GetDefaultModel()
	cfg.Agents.Defaults.Workspace = c.GetDefaultWorkspace()
	cfg.Agents.Defaults.ModelName = modelName

	providerConfigs := GetProviderConfigFromDir(sourceHome)
	defaultAPIKey := ""
	defaultBaseURL := ""

	if provCfg, ok := providerConfigs[provider]; ok {
		defaultAPIKey = provCfg.ApiKey
		defaultBaseURL = provCfg.BaseUrl
	}

	cfg.ModelList = []ModelConfig{
		{
			ModelName: modelName,
			Model:     fmt.Sprintf("%s/%s", provider, modelName),
			APIKey:    defaultAPIKey,
			APIBase:   defaultBaseURL,
		},
	}

	for provName, provCfg := range providerConfigs {
		if provName == provider {
			continue
		}
		if provCfg.ApiKey != "" {
			continue
		}
		cfg.ModelList = append(cfg.ModelList, ModelConfig{
			ModelName: fmt.Sprintf("%s", provName),
			Model:     fmt.Sprintf("%s/%s", provName, provName),
			APIKey:    provCfg.ApiKey,
			APIBase:   provCfg.BaseUrl,
		})
	}

	cfg.Channels = c.convertChannels(&warnings)

	agentList := c.convertAgents(&warnings)
	if len(agentList) > 0 {
		cfg.Agents.List = agentList
	}

	if c.HasSkills() {
		warnings = append(
			warnings,
			fmt.Sprintf(
				"Skills (%d entries) not automatically migrated - reinstall via kuromatsu CLI",
				len(c.Skills.Entries),
			),
		)
	}
	if c.HasMemory() {
		warnings = append(warnings, "Memory backend config not migrated - Kuromatsu uses SQLite with vector embeddings")
	}
	if c.HasCron() {
		warnings = append(
			warnings,
			"Cron job scheduling not supported in Kuromatsu - consider using external schedulers",
		)
	}
	if c.HasHooks() {
		warnings = append(warnings, "Webhook hooks not supported in Kuromatsu - use event system instead")
	}
	if c.HasSession() {
		warnings = append(warnings, "Session scope config differs - Kuromatsu uses per-agent sessions by default")
	}
	if c.HasAuthProfiles() {
		warnings = append(
			warnings,
			"Auth profiles (API keys, OAuth tokens) not migrated for security - set env vars manually",
		)
	}

	return cfg, warnings, nil
}

type ModelConfig struct {
	ModelName string `json:"model_name"`
	Model     string `json:"model"`
	APIBase   string `json:"api_base,omitempty"`
	APIKey    string `json:"api_key"`
	Proxy     string `json:"proxy,omitempty"`
}

type KuromatsuConfig struct {
	Agents    AgentsConfig   `json:"agents"`
	Bindings  []AgentBinding `json:"bindings,omitempty"`
	Channels  ChannelsConfig `json:"channels"`
	ModelList []ModelConfig  `json:"model_list"`
	Gateway   GatewayConfig  `json:"gateway"`
	Tools     ToolsConfig    `json:"tools"`
}

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
	List     []AgentConfig `json:"list,omitempty"`
}

type AgentDefaults struct {
	Workspace           string   `json:"workspace"`
	RestrictToWorkspace bool     `json:"restrict_to_workspace"`
	Provider            string   `json:"provider"`
	ModelName           string   `json:"model_name"`
	Model               string   `json:"model,omitempty"`
	ModelFallbacks      []string `json:"model_fallbacks,omitempty"`
	ImageModel          string   `json:"image_model,omitempty"`
	ImageModelFallbacks []string `json:"image_model_fallbacks,omitempty"`
	MaxTokens           int      `json:"max_tokens"`
	Temperature         *float64 `json:"temperature,omitempty"`
	MaxToolIterations   int      `json:"max_tool_iterations"`
}

type AgentConfig struct {
	ID        string            `json:"id"`
	Default   bool              `json:"default,omitempty"`
	Name      string            `json:"name,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
	Model     *AgentModelConfig `json:"model,omitempty"`
	Skills    []string          `json:"skills,omitempty"`
}

type AgentModelConfig struct {
	Primary   string   `json:"primary,omitempty"`
	Fallbacks []string `json:"fallbacks,omitempty"`
}

type AgentBinding struct {
	AgentID string       `json:"agent_id"`
	Match   BindingMatch `json:"match"`
}

type BindingMatch struct {
	Channel   string     `json:"channel"`
	AccountID string     `json:"account_id,omitempty"`
	Peer      *PeerMatch `json:"peer,omitempty"`
	GuildID   string     `json:"guild_id,omitempty"`
	TeamID    string     `json:"team_id,omitempty"`
}

type PeerMatch struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type ChannelsConfig struct {
	WhatsApp *WhatsAppConfig `json:"whatsapp,omitempty"`
	Telegram *TelegramConfig `json:"telegram,omitempty"`
}

type WhatsAppConfig struct {
	Enabled   bool     `json:"enabled"`
	BridgeURL string   `json:"bridge_url"`
	AllowFrom []string `json:"allow_from"`
}

type TelegramConfig struct {
	Enabled       bool     `json:"enabled"`
	Token         string   `json:"token"`
	Proxy         string   `json:"proxy"`
	AllowFrom     []string `json:"allow_from"`
	UseMarkdownV2 bool     `json:"use_markdown_v2"`
}

type GatewayConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type ToolsConfig struct {
	Web  WebToolsConfig `json:"web"`
	Cron CronConfig     `json:"cron"`
	Exec ExecConfig     `json:"exec"`
}

type WebToolsConfig struct {
	Brave      BraveConfig      `json:"brave"`
	Tavily     TavilyConfig     `json:"tavily"`
	DuckDuckGo DuckDuckGoConfig `json:"duckduckgo"`
	Perplexity PerplexityConfig `json:"perplexity"`
	Proxy      string           `json:"proxy,omitempty"`
}

type BraveConfig struct {
	Enabled    bool     `json:"enabled"`
	APIKey     string   `json:"api_key"`
	APIKeys    []string `json:"api_keys"`
	MaxResults int      `json:"max_results"`
}

type TavilyConfig struct {
	Enabled    bool     `json:"enabled"`
	APIKey     string   `json:"api_key"`
	APIKeys    []string `json:"api_keys"`
	BaseURL    string   `json:"base_url"`
	MaxResults int      `json:"max_results"`
}

type DuckDuckGoConfig struct {
	Enabled    bool `json:"enabled"`
	MaxResults int  `json:"max_results"`
}

type PerplexityConfig struct {
	Enabled    bool     `json:"enabled"`
	APIKey     string   `json:"api_key"`
	APIKeys    []string `json:"api_keys"`
	MaxResults int      `json:"max_results"`
}

type CronConfig struct {
	ExecTimeoutMinutes int `json:"exec_timeout_minutes"`
}

type ExecConfig struct {
	EnableDenyPatterns bool     `json:"enable_deny_patterns"`
	CustomDenyPatterns []string `json:"custom_deny_patterns"`
}

func (c *OpenClawConfig) convertChannels(warnings *[]string) ChannelsConfig {
	channels := ChannelsConfig{}

	if c.Channels == nil {
		return channels
	}

	if t := c.Channels.Telegram; t != nil {
		tg := &TelegramConfig{
			Enabled:       t.Enabled == nil || *t.Enabled,
			AllowFrom:     t.AllowFrom,
			UseMarkdownV2: t.UseMarkdownV2 != nil && *t.UseMarkdownV2,
		}
		if t.BotToken != nil {
			tg.Token = *t.BotToken
		}
		channels.Telegram = tg
		warnEmptyAllowFrom(warnings, "telegram", tg.AllowFrom)
	}

	if w := c.Channels.WhatsApp; w != nil {
		wa := &WhatsAppConfig{
			Enabled:   w.Enabled == nil || *w.Enabled,
			AllowFrom: w.AllowFrom,
		}
		if w.BridgeURL != nil {
			wa.BridgeURL = *w.BridgeURL
		}
		channels.WhatsApp = wa
		warnEmptyAllowFrom(warnings, "whatsapp", wa.AllowFrom)
	}

	// Channels present in the source that Kuromatsu cannot run are reported, never emitted.
	for _, name := range slices.Sorted(maps.Keys(c.Channels.Others)) {
		if string(c.Channels.Others[name]) == "null" { // a null entry means the channel is absent
			continue
		}
		*warnings = append(*warnings, fmt.Sprintf("Channel '%s': not supported by Kuromatsu; ignored", name))
	}

	return channels
}

func warnEmptyAllowFrom(warnings *[]string, name string, allowFrom []string) {
	if len(allowFrom) == 0 {
		*warnings = append(*warnings, fmt.Sprintf(
			"Channel '%s': allow_from is empty; it will reject all senders until you set it", name))
	}
}

func (c *OpenClawConfig) convertAgents(warnings *[]string) []AgentConfig {
	var agents []AgentConfig

	if c.Agents == nil {
		return agents
	}

	for _, entry := range c.Agents.List {
		agentID := entry.ID
		if agentID == "" {
			continue
		}

		agentName := agentID
		if entry.Name != nil {
			agentName = *entry.Name
		}

		agentCfg := AgentConfig{
			ID:      agentID,
			Name:    agentName,
			Default: len(agents) == 0,
		}

		if entry.Workspace != nil {
			agentCfg.Workspace = rewriteWorkspacePath(*entry.Workspace)
		}

		if entry.Model != nil {
			primary := entry.Model.GetPrimary()
			if primary != "" {
				agentCfg.Model = &AgentModelConfig{
					Primary:   primary,
					Fallbacks: entry.Model.GetFallbacks(),
				}
			}
		}

		if len(entry.Skills) > 0 {
			agentCfg.Skills = entry.Skills
		}

		agents = append(agents, agentCfg)
	}

	return agents
}

func (c *KuromatsuConfig) ToStandardConfig() *config.Config {
	cfg := config.DefaultConfig()

	cfg.Agents.Defaults.Workspace = c.Agents.Defaults.Workspace
	cfg.Agents.Defaults.Provider = c.Agents.Defaults.Provider
	cfg.Agents.Defaults.ModelName = c.Agents.Defaults.ModelName
	cfg.Agents.Defaults.ModelFallbacks = c.Agents.Defaults.ModelFallbacks

	for _, m := range c.ModelList {
		mc := &config.ModelConfig{
			ModelName: m.ModelName,
			Model:     m.Model,
			APIBase:   m.APIBase,
			Proxy:     m.Proxy,
		}
		if m.APIKey != "" {
			mc.SetAPIKey(m.APIKey)
		}
		cfg.ModelList = append(cfg.ModelList, mc)
	}

	cfg.Channels = c.Channels.ToStandardChannels()
	cfg.Gateway = c.Gateway.ToStandardGateway()
	cfg.Tools = c.Tools.ToStandardTools()

	cfg.Agents.List = make([]config.AgentConfig, len(c.Agents.List))
	for i, a := range c.Agents.List {
		cfg.Agents.List[i] = config.AgentConfig{
			ID:        a.ID,
			Default:   a.Default,
			Name:      a.Name,
			Workspace: a.Workspace,
			Skills:    a.Skills,
		}
		if a.Model != nil {
			cfg.Agents.List[i].Model = &config.AgentModelConfig{
				Primary:   a.Model.Primary,
				Fallbacks: a.Model.Fallbacks,
			}
		}
	}

	return cfg
}

func (c ChannelsConfig) ToStandardChannels() config.ChannelsConfig {
	channels := make(config.ChannelsConfig)

	if wa := c.WhatsApp; wa != nil {
		setChannel(channels, "whatsapp", map[string]any{
			"enabled":    wa.Enabled,
			"allow_from": allowFromOrEmpty(wa.AllowFrom),
			"bridge_url": wa.BridgeURL,
		})
	}

	if tg := c.Telegram; tg != nil {
		m := map[string]any{
			"enabled":         tg.Enabled,
			"allow_from":      allowFromOrEmpty(tg.AllowFrom),
			"proxy":           tg.Proxy,
			"use_markdown_v2": tg.UseMarkdownV2,
		}
		if tg.Token != "" {
			m["token"] = config.NewSecureString(tg.Token)
		}
		setChannel(channels, "telegram", m)
	}

	return channels
}

func allowFromOrEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func setChannel(channels config.ChannelsConfig, name string, cfg any) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	// Wrap in "settings" for nested format
	var m map[string]any
	if err = json.Unmarshal(data, &m); err != nil {
		return
	}
	settings := make(map[string]any)
	for k, v := range m {
		if _, exists := config.BaseFieldNames[k]; !exists {
			settings[k] = v
			delete(m, k)
		}
	}
	if len(settings) > 0 {
		m["settings"] = settings
	}
	nestedData, err := json.Marshal(m)
	if err != nil {
		return
	}
	bc := &config.Channel{}
	if err := json.Unmarshal(nestedData, bc); err != nil {
		return
	}
	channels[name] = bc
}

func (c GatewayConfig) ToStandardGateway() config.GatewayConfig {
	return config.GatewayConfig{
		Host: c.Host,
		Port: c.Port,
	}
}

func (c ToolsConfig) ToStandardTools() config.ToolsConfig {
	brave := config.BraveConfig{
		Enabled:    c.Web.Brave.Enabled,
		MaxResults: c.Web.Brave.MaxResults,
	}
	if c.Web.Brave.APIKey != "" {
		brave.SetAPIKey(c.Web.Brave.APIKey)
	}
	if len(c.Web.Brave.APIKeys) > 0 {
		brave.SetAPIKeys(c.Web.Brave.APIKeys)
	}

	tavily := config.TavilyConfig{
		Enabled:    c.Web.Tavily.Enabled,
		BaseURL:    c.Web.Tavily.BaseURL,
		MaxResults: c.Web.Tavily.MaxResults,
	}
	if c.Web.Tavily.APIKey != "" {
		tavily.SetAPIKey(c.Web.Tavily.APIKey)
	}

	perplexity := config.PerplexityConfig{
		Enabled:    c.Web.Perplexity.Enabled,
		MaxResults: c.Web.Perplexity.MaxResults,
	}
	if c.Web.Perplexity.APIKey != "" {
		perplexity.SetAPIKey(c.Web.Perplexity.APIKey)
	}

	return config.ToolsConfig{
		Web: config.WebToolsConfig{
			Brave:  brave,
			Tavily: tavily,
			DuckDuckGo: config.DuckDuckGoConfig{
				Enabled:    c.Web.DuckDuckGo.Enabled,
				MaxResults: c.Web.DuckDuckGo.MaxResults,
			},
			Perplexity: perplexity,
			Proxy:      c.Web.Proxy,
		},
		Cron: config.CronToolsConfig{
			ExecTimeoutMinutes: c.Cron.ExecTimeoutMinutes,
		},
		Exec: config.ExecConfig{
			EnableDenyPatterns: c.Exec.EnableDenyPatterns,
			CustomDenyPatterns: c.Exec.CustomDenyPatterns,
			AllowRemote:        config.DefaultConfig().Tools.Exec.AllowRemote,
		},
	}
}
