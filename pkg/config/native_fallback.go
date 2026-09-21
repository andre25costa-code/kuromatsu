package config

import (
	"slices"

	providercommon "github.com/andre25costa-code/kuromatsu/pkg/providers/common"
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

// NativeLimits reads the native-model runtime knobs (n_ctx, max_predict)
// from a model config's ExtraBody — the same keys
// pkg/providers/native_options.go's nativeOptionsFromModelConfig reads to
// build localllm.Options, kept in sync deliberately so
// ContextWindow/MaxTokens (FR-015/AC-015-6) never drift from what the
// engine actually loaded with. ok is false when mc is nil or ExtraBody sets
// neither key — nothing for the caller to apply, e.g. every non-native
// model, which never sets extra_body.n_ctx/max_predict.
func NativeLimits(mc *ModelConfig) (nCtx, maxPredict int, ok bool) {
	if mc == nil || mc.ExtraBody == nil {
		return 0, 0, false
	}
	if v, found := providercommon.AsInt(mc.ExtraBody["n_ctx"]); found {
		nCtx = v
		ok = true
	}
	if v, found := providercommon.AsInt(mc.ExtraBody["max_predict"]); found {
		maxPredict = v
		ok = true
	}
	return nCtx, maxPredict, ok
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
