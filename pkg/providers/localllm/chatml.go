package localllm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/providers/protocoltypes"
)

const (
	imStart = "<|im_start|>"
	imEnd   = "<|im_end|>"

	toolsHeader = "\n\n# Tools\n\nYou may call one or more functions to assist with the user query.\n\n" +
		"You are provided with function signatures within <tools></tools> XML tags:\n<tools>"
	toolsFooter = "</tools>\n\nFor each function call, return a json object with function name and " +
		"arguments within <tool_call></tool_call> XML tags:\n<tool_call>\n" +
		`{"name": <function-name>, "arguments": <args-json-object>}` + "\n</tool_call>"

	maxParsedToolCalls = 4
)

// toolJSON/toolFunctionJSON fix the field order of the per-tool JSON line
// inside <tools>...</tools> (type, then name/description/parameters) to
// match the fixed template structure. A plain map[string]any would work
// too, but encoding/json sorts map keys alphabetically, which would
// reorder "type" after "function" and drift from the training-time shape.
type toolJSON struct {
	Type     string           `json:"type"`
	Function toolFunctionJSON `json:"function"`
}

type toolFunctionJSON struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// RenderPrompt renders the ChatML/Qwen3 prompt baked into the Bonsai GGUF's
// tokenizer.chat_template (ADR-004, FR-002). llama_chat_apply_template
// cannot do this because it has no tools parameter and does not use a
// jinja parser, so the template is reproduced here in Go.
//
// enableThinking controls whether the assistant turn is primed with an
// empty <think></think> block, which suppresses the 1.7B model's thinking
// spend (S18).
func RenderPrompt(messages []protocoltypes.Message, tools []protocoltypes.ToolDefinition, enableThinking bool) string {
	var b strings.Builder

	systemContent := ""
	hadSystemMsg := len(messages) > 0 && messages[0].Role == "system"
	rest := messages
	if hadSystemMsg {
		systemContent = messages[0].Content
		rest = messages[1:]
	}

	if hadSystemMsg || len(tools) > 0 {
		b.WriteString(imStart + "system\n")
		b.WriteString(systemContent)
		if len(tools) > 0 {
			b.WriteString(toolsHeader)
			b.WriteByte('\n')
			for _, t := range tools {
				line, err := json.Marshal(toolJSON{
					Type: "function",
					Function: toolFunctionJSON{
						Name:        t.Function.Name,
						Description: t.Function.Description,
						Parameters:  t.Function.Parameters,
					},
				})
				if err != nil {
					continue
				}
				b.Write(line)
				b.WriteByte('\n')
			}
			b.WriteString(toolsFooter)
		}
		b.WriteString(imEnd + "\n")
	}

	i := 0
	for i < len(rest) {
		msg := rest[i]

		if msg.Role == "tool" {
			b.WriteString(imStart + "user")
			for i < len(rest) && rest[i].Role == "tool" {
				b.WriteString("\n<tool_response>\n")
				b.WriteString(rest[i].Content)
				b.WriteString("\n</tool_response>")
				i++
			}
			b.WriteString(imEnd + "\n")
			continue
		}

		if len(msg.ToolCalls) > 0 {
			b.WriteString(imStart + msg.Role)
			if msg.Content != "" {
				b.WriteString("\n" + msg.Content)
			}
			for _, tc := range msg.ToolCalls {
				args := tc.Arguments
				if args == nil {
					args = map[string]any{}
				}
				argsJSON, err := json.Marshal(args)
				if err != nil {
					argsJSON = []byte("{}")
				}
				fmt.Fprintf(&b, "\n<tool_call>\n{\"name\": %q, \"arguments\": %s}\n</tool_call>", tc.Name, argsJSON)
			}
			b.WriteString(imEnd + "\n")
			i++
			continue
		}

		b.WriteString(imStart + msg.Role + "\n" + msg.Content + imEnd + "\n")
		i++
	}

	b.WriteString(imStart + "assistant\n")
	if !enableThinking {
		b.WriteString("<think>\n\n</think>\n\n")
	}

	return b.String()
}

// ParseOutput extracts reasoning (<think>...</think>) and tool calls
// (<tool_call>{json}</tool_call>) from a raw model completion, returning the
// remaining text as content. Malformed tool_call JSON falls back to
// Arguments{"raw": <text>} (parity with openai_compat's streaming decode
// fallback) rather than dropping the call or erroring. AC-002-2..5.
func ParseOutput(raw string) (content, reasoning string, calls []protocoltypes.ToolCall) {
	text := raw

	if start := strings.Index(text, "<think>"); start >= 0 {
		if end := strings.Index(text[start:], "</think>"); end >= 0 {
			absEnd := start + end
			reasoning = strings.TrimSpace(text[start+len("<think>") : absEnd])
			text = text[:start] + text[absEnd+len("</think>"):]
		}
	}

	const openTag, closeTag = "<tool_call>", "</tool_call>"
	var b strings.Builder
	rest := text
	for len(calls) < maxParsedToolCalls {
		start := strings.Index(rest, openTag)
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		afterOpen := rest[start+len(openTag):]

		end := strings.Index(afterOpen, closeTag)
		if end < 0 {
			// Unterminated tool_call at EOG: treat the tag and everything
			// after it as ordinary content rather than losing it.
			b.WriteString(rest[start:])
			rest = ""
			break
		}

		block := strings.TrimSpace(afterOpen[:end])
		calls = append(calls, parseToolCallBlock(len(calls), block))
		rest = afterOpen[end+len(closeTag):]
	}
	if rest != "" && len(calls) >= maxParsedToolCalls {
		b.WriteString(rest)
	}

	content = strings.TrimSpace(b.String())
	return content, reasoning, calls
}

func parseToolCallBlock(index int, block string) protocoltypes.ToolCall {
	var decoded struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}

	id := fmt.Sprintf("call_%d", index)

	if err := json.Unmarshal([]byte(block), &decoded); err != nil {
		return protocoltypes.ToolCall{ID: id, Arguments: map[string]any{"raw": block}}
	}

	args := map[string]any{}
	if len(decoded.Arguments) > 0 {
		if err := json.Unmarshal(decoded.Arguments, &args); err != nil {
			// arguments may be double-encoded as a JSON string.
			var asString string
			if err2 := json.Unmarshal(decoded.Arguments, &asString); err2 == nil {
				if err3 := json.Unmarshal([]byte(asString), &args); err3 != nil {
					return protocoltypes.ToolCall{ID: id, Name: decoded.Name, Arguments: map[string]any{"raw": block}}
				}
			} else {
				return protocoltypes.ToolCall{ID: id, Name: decoded.Name, Arguments: map[string]any{"raw": block}}
			}
		}
	}

	return protocoltypes.ToolCall{ID: id, Name: decoded.Name, Arguments: args}
}
