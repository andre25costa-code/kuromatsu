package providers

import (
	"strings"
)

// IsSupportedModelProvider reports whether provider resolves to a provider ID
// returned by ModelProviderOptions.
func IsSupportedModelProvider(provider string) bool {
	_, ok := modelProviderOptionForName(provider)
	return ok
}

// SplitModelProviderAndID separates a legacy "provider/model" string into its
// effective provider and canonical model ID. Unknown prefixes are treated as
// part of the model ID and fall back to defaultProvider.
func SplitModelProviderAndID(model, defaultProvider string) (provider, modelID string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", ""
	}

	provider, modelID = splitKnownProviderModel(model)
	if provider != "" || modelID != "" {
		return provider, modelID
	}

	return NormalizeProvider(defaultProvider), model
}

func splitKnownProviderModel(model string) (provider, modelID string) {
	provider, modelID, found := strings.Cut(strings.TrimSpace(model), "/")
	if !found {
		return "", ""
	}
	provider = strings.TrimSpace(provider)
	modelID = strings.TrimSpace(modelID)
	if provider == "" {
		return "", modelID
	}
	if !IsSupportedModelProvider(provider) {
		return "", ""
	}
	return NormalizeProvider(provider), modelID
}
