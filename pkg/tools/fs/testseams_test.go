package fstools

import (
	"context"

	toolshared "github.com/andre25costa-code/kuromatsu/pkg/tools/shared"
)

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

func WithToolContext(ctx context.Context, channel, chatID string) context.Context {
	return toolshared.WithToolContext(ctx, channel, chatID)
}
