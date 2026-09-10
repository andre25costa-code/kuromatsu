package config

import (
	"slices"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/localllm"
)

// nativeModelName is the model_name of the seeded "native" model_list entry
// (see defaults.go). It is also the string agents.defaults.model_name and
// agents.defaults.model_fallbacks reference to select it, matching the
// existing convention for other local providers such as "local-model".
const nativeModelName = "bonsai-local"

// nativeBuilt is a seam over localllm.Built so tests can exercise
// ApplyNativeFallback's wiring logic (AC-003-1/-2) without requiring an
// actual nativellm+cgo build; production code always goes through the real
// localllm.Built().
var nativeBuilt = localllm.Built

// ApplyNativeFallback wires the seeded native-inference model entry into
// the runtime config so the agent always has a usable model even with no
// API keys configured (FR-003, ADR-002). Called at the end of LoadConfig.
//
// It is a strict no-op unless the binary was built with the nativellm cgo
// engine and the GGUF file actually exists on disk (BR-002): a pure-Go
// build, or one missing the model file, never gets a dead candidate added
// to its fallback chain.
func ApplyNativeFallback(cfg *Config) {
	if cfg == nil || !nativeBuilt() {
		return
	}

	entry := findModelConfigByName(cfg.ModelList, nativeModelName)
	if entry == nil {
		// The user removed or renamed the seeded entry; nothing to wire.
		return
	}

	if _, err := localllm.ResolveModelPath(entry.Model, GetHome()); err != nil {
		return
	}

	entry.Enabled = true

	if cfg.Agents.Defaults.ModelName == "" && !anyOtherModelHasAPIKey(cfg.ModelList, entry) {
		cfg.Agents.Defaults.ModelName = nativeModelName
		return
	}

	if !slices.Contains(cfg.Agents.Defaults.ModelFallbacks, nativeModelName) {
		cfg.Agents.Defaults.ModelFallbacks = append(cfg.Agents.Defaults.ModelFallbacks, nativeModelName)
	}
}

func findModelConfigByName(models []*ModelConfig, name string) *ModelConfig {
	for _, m := range models {
		if m != nil && m.ModelName == name {
			return m
		}
	}
	return nil
}

func anyOtherModelHasAPIKey(models []*ModelConfig, exclude *ModelConfig) bool {
	for _, m := range models {
		if m == nil || m == exclude {
			continue
		}
		if m.APIKey() != "" {
			return true
		}
	}
	return false
}
