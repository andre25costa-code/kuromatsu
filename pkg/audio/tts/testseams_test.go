package tts

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

func NewOpenAITTSProvider(apiKey string, apiBase string, proxyURL string, model string) *OpenAITTSProvider {
	return NewOpenAITTSProviderWithOptions(apiKey, apiBase, proxyURL, model, OpenAITTSOptions{})
}
