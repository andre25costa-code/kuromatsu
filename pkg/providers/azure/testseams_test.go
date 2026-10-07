package azure

import (
	"context"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
)

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

// NewProviderWithTokenSource creates a new Azure OpenAI provider that obtains its
// bearer token from the supplied callback on every request. Used for Entra ID auth
// where tokens are short-lived and refreshed by the underlying credential.
func NewProviderWithTokenSource(
	apiBase, proxy, userAgent string,
	tokenSource func(ctx context.Context) (string, error),
	opts ...Option,
) *Provider {
	p := &Provider{
		apiBase:     strings.TrimRight(apiBase, "/"),
		userAgent:   userAgent,
		httpClient:  common.NewHTTPClient(proxy),
		tokenSource: tokenSource,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}

	return p
}

// WithTokenSource sets a callback that returns a bearer token per request.
// When set, it takes precedence over the static api key.
func WithTokenSource(ts func(ctx context.Context) (string, error)) Option {
	return func(p *Provider) {
		p.tokenSource = ts
	}
}
