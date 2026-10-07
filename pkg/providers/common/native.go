package common

import "strings"

// NativeProviderAliases is shared by configuration validation and the catalog.
func NativeProviderAliases() []string { return []string{"bonsai", "kuro"} }

func IsNativeModel(provider, model string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(model)), "/")
	}
	if provider == "native" {
		return true
	}
	for _, alias := range NativeProviderAliases() {
		if provider == alias {
			return true
		}
	}
	return false
}
