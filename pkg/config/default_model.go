package config

import "encoding/json"

// inferModelEnabledFromKeys keeps the ModelConfig.Enabled contract for a
// current-version config: an entry whose JSON omits "enabled" is enabled
// when it has an API key (from config.json or the security file). Only the
// V1->V2 migration did this, so a key added to a seeded entry left the model
// disabled -- hidden from `kuromatsu model` and refused as a default. An
// explicit "enabled": false is kept. data is the config.json the cfg was
// loaded from.
func inferModelEnabledFromKeys(data []byte, cfg *Config) {
	if cfg == nil {
		return
	}
	var raw struct {
		ModelList []struct {
			ModelName string `json:"model_name"`
			Enabled   *bool  `json:"enabled"`
		} `json:"model_list"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	omitted := make(map[string]bool, len(raw.ModelList))
	for _, m := range raw.ModelList {
		if m.Enabled == nil {
			omitted[m.ModelName] = true
		}
	}
	for _, m := range cfg.ModelList {
		if m != nil && !m.Enabled && omitted[m.ModelName] && m.APIKey() != "" {
			m.Enabled = true
		}
	}
}

// applySoleKeyedDefault makes the only enabled model with an API key the
// default when no default is set. onboard leaves model_name empty, so a user
// who adds a single key would otherwise hit `model "" not found` on the first
// message. Two or more keyed models are an explicit choice the user must make
// (`kuromatsu model <name>`). Called by LoadConfig before ApplyNativeFallback,
// which then adds the native model as this default's fallback.
func applySoleKeyedDefault(cfg *Config) {
	if cfg == nil || cfg.Agents.Defaults.ModelName != "" {
		return
	}
	var sole *ModelConfig
	for _, m := range cfg.ModelList {
		if m == nil || m.IsVirtual() || !m.Enabled || m.APIKey() == "" {
			continue
		}
		if sole != nil {
			return
		}
		sole = m
	}
	if sole != nil {
		cfg.Agents.Defaults.ModelName = sole.ModelName
	}
}
