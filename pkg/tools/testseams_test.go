package tools

import (
	"context"
	"regexp"

	toolshared "github.com/andre25costa-code/kuromatsu/pkg/tools/shared"
)

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

func WithToolMessageContext(ctx context.Context, messageID, replyToMessageID string) context.Context {
	return toolshared.WithToolMessageContext(ctx, messageID, replyToMessageID)
}

func NewExecTool(workingDir string, restrict bool, allowPaths ...[]*regexp.Regexp) (*ExecTool, error) {
	return NewExecToolWithConfig(workingDir, restrict, nil, allowPaths...)
}
