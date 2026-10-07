package integrationtools

import (
	"context"

	toolshared "github.com/andre25costa-code/kuromatsu/pkg/tools/shared"
)

type (
	Tool          = toolshared.Tool
	ToolResult    = toolshared.ToolResult
	AsyncCallback = toolshared.AsyncCallback
)

func ToolChannel(ctx context.Context) string {
	return toolshared.ToolChannel(ctx)
}

func ToolChatID(ctx context.Context) string {
	return toolshared.ToolChatID(ctx)
}

func ToolMessageID(ctx context.Context) string {
	return toolshared.ToolMessageID(ctx)
}

func ToolSessionKey(ctx context.Context) string {
	return toolshared.ToolSessionKey(ctx)
}

func ErrorResult(message string) *ToolResult {
	return toolshared.ErrorResult(message)
}

func SilentResult(forLLM string) *ToolResult {
	return toolshared.SilentResult(forLLM)
}
