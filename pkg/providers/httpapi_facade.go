package providers

import httpapi "github.com/andre25costa-code/kuromatsu/pkg/providers/httpapi"

type (
	GeminiProvider = httpapi.GeminiProvider
	HTTPProvider   = httpapi.HTTPProvider
)

func NewGeminiProvider(
	apiKey string,
	apiBase string,
	proxy string,
	userAgent string,
	requestTimeoutSeconds int,
	extraBody map[string]any,
	customHeaders map[string]string,
) *GeminiProvider {
	return httpapi.NewGeminiProvider(apiKey, apiBase, proxy, userAgent, requestTimeoutSeconds, extraBody, customHeaders)
}

func NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(
	apiKey, apiBase, proxy, maxTokensField, userAgent string,
	requestTimeoutSeconds int,
	extraBody map[string]any,
	customHeaders map[string]string,
) *HTTPProvider {
	return httpapi.NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(
		apiKey,
		apiBase,
		proxy,
		maxTokensField,
		userAgent,
		requestTimeoutSeconds,
		extraBody,
		customHeaders,
	)
}
