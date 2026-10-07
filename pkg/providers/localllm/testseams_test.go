package localllm

import "github.com/andre25costa-code/kuromatsu/pkg/providers/protocoltypes"

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

// RenderPrompt renders the ChatML/Qwen3 prompt baked into the Bonsai GGUF's
// tokenizer.chat_template (ADR-004, FR-002). llama_chat_apply_template
// cannot do this because it has no tools parameter and does not use a
// jinja parser, so the template is reproduced here in Go.
//
// enableThinking controls whether the assistant turn is primed with an
// empty <think></think> block, which suppresses the 1.7B model's thinking
// spend (S18).
func RenderPrompt(messages []protocoltypes.Message, tools []protocoltypes.ToolDefinition, enableThinking bool) string {
	prompt, _ := RenderPromptParts(messages, tools, enableThinking)
	return prompt
}
