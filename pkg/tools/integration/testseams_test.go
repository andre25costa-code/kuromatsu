package integrationtools

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/session"
	toolshared "github.com/andre25costa-code/kuromatsu/pkg/tools/shared"
)

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

func WithToolContext(ctx context.Context, channel, chatID string) context.Context {
	return toolshared.WithToolContext(ctx, channel, chatID)
}

func ToolAgentID(ctx context.Context) string {
	return toolshared.ToolAgentID(ctx)
}

func ToolSessionScope(ctx context.Context) *session.SessionScope {
	return toolshared.ToolSessionScope(ctx)
}

func WithToolSessionContext(
	ctx context.Context,
	agentID, sessionKey string,
	scope *session.SessionScope,
) context.Context {
	return toolshared.WithToolSessionContext(ctx, agentID, sessionKey, scope)
}

func WithToolInboundContext(
	ctx context.Context,
	channel, chatID, messageID, replyToMessageID string,
) context.Context {
	return toolshared.WithToolInboundContext(ctx, channel, chatID, messageID, replyToMessageID)
}

func NewWebFetchTool(maxChars int, format string, fetchLimitBytes int64) (*WebFetchTool, error) {
	// createHTTPClient cannot fail with an empty proxy string.
	return NewWebFetchToolWithConfig(maxChars, "", format, fetchLimitBytes, nil)
}
