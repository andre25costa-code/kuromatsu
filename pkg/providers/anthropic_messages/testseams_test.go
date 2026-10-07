package anthropicmessages

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

// NewProvider creates a new Anthropic Messages API provider.
func NewProvider(apiKey, apiBase, userAgent string) *Provider {
	return NewProviderWithTimeout(apiKey, apiBase, userAgent, 0)
}
