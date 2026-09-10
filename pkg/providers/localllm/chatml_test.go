package localllm

import (
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/protocoltypes"
)

func TestRenderPrompt_SystemAndUser_NoTools_ThinkingDisabled(t *testing.T) {
	messages := []protocoltypes.Message{
		{Role: "system", Content: "Você é útil."},
		{Role: "user", Content: "Oi"},
	}

	got := RenderPrompt(messages, nil, false)
	want := "<|im_start|>system\n" + "Você é útil." + "<|im_end|>\n" +
		"<|im_start|>user\n" + "Oi" + "<|im_end|>\n" +
		"<|im_start|>assistant\n" + "<think>\n\n</think>\n\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderPrompt_ThinkingEnabled_NoPrefill(t *testing.T) {
	messages := []protocoltypes.Message{{Role: "user", Content: "Oi"}}

	got := RenderPrompt(messages, nil, true)
	want := "<|im_start|>user\nOi<|im_end|>\n<|im_start|>assistant\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderPrompt_NoSystemMessage_NoTools(t *testing.T) {
	messages := []protocoltypes.Message{{Role: "user", Content: "Oi"}}

	got := RenderPrompt(messages, nil, false)
	want := "<|im_start|>user\nOi<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderPrompt_WithTools(t *testing.T) {
	messages := []protocoltypes.Message{
		{Role: "system", Content: "Sistema."},
		{Role: "user", Content: "Que horas são em Tóquio?"},
	}
	tools := []protocoltypes.ToolDefinition{
		{
			Type: "function",
			Function: protocoltypes.ToolFunctionDefinition{
				Name:        "get_time",
				Description: "Retorna a hora atual num fuso.",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"timezone": map[string]any{"type": "string"}},
					"required":   []any{"timezone"},
				},
			},
		},
	}

	got := RenderPrompt(messages, tools, false)

	toolLine := `{"type":"function","function":{"name":"get_time","description":"Retorna a hora atual num fuso.",` +
		`"parameters":{"properties":{"timezone":{"type":"string"}},"required":["timezone"],"type":"object"}}}`

	want := "<|im_start|>system\n" + "Sistema." +
		"\n\n# Tools\n\nYou may call one or more functions to assist with the user query.\n\n" +
		"You are provided with function signatures within <tools></tools> XML tags:\n<tools>\n" +
		toolLine + "\n</tools>\n\n" +
		"For each function call, return a json object with function name and arguments within " +
		"<tool_call></tool_call> XML tags:\n<tool_call>\n" +
		`{"name": <function-name>, "arguments": <args-json-object>}` + "\n</tool_call>" +
		"<|im_end|>\n" +
		"<|im_start|>user\nQue horas são em Tóquio?<|im_end|>\n" +
		"<|im_start|>assistant\n<think>\n\n</think>\n\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderPrompt_AssistantWithSingleToolCall_EmptyContent(t *testing.T) {
	messages := []protocoltypes.Message{
		{Role: "user", Content: "Que horas são?"},
		{
			Role: "assistant",
			ToolCalls: []protocoltypes.ToolCall{
				{Name: "get_time", Arguments: map[string]any{"timezone": "Asia/Tokyo"}},
			},
		},
	}

	got := RenderPrompt(messages, nil, true)
	want := "<|im_start|>user\nQue horas são?<|im_end|>\n" +
		`<|im_start|>assistant` + "\n<tool_call>\n" +
		`{"name": "get_time", "arguments": {"timezone":"Asia/Tokyo"}}` + "\n</tool_call>" +
		"<|im_end|>\n" +
		"<|im_start|>assistant\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderPrompt_AssistantWithTwoToolCalls_EmptyContent(t *testing.T) {
	messages := []protocoltypes.Message{
		{
			Role: "assistant",
			ToolCalls: []protocoltypes.ToolCall{
				{Name: "a", Arguments: map[string]any{}},
				{Name: "b", Arguments: map[string]any{}},
			},
		},
	}

	got := RenderPrompt(messages, nil, true)
	// The separator between two consecutive tool_call blocks must be exactly
	// one newline, regardless of Content being empty (this is the bug this
	// test guards against: an earlier draft only inserted the separator
	// when msg.Content was non-empty).
	want := "<|im_start|>assistant" +
		"\n<tool_call>\n" + `{"name": "a", "arguments": {}}` + "\n</tool_call>" +
		"\n<tool_call>\n" + `{"name": "b", "arguments": {}}` + "\n</tool_call>" +
		"<|im_end|>\n" +
		"<|im_start|>assistant\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderPrompt_SingleToolResponse(t *testing.T) {
	messages := []protocoltypes.Message{
		{Role: "tool", Content: `{"temp_c": 22}`},
	}

	got := RenderPrompt(messages, nil, true)
	want := "<|im_start|>user" +
		"\n<tool_response>\n" + `{"temp_c": 22}` + "\n</tool_response>" +
		"<|im_end|>\n" +
		"<|im_start|>assistant\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderPrompt_ConsecutiveToolResponsesMerged(t *testing.T) {
	messages := []protocoltypes.Message{
		{Role: "tool", Content: "resultado 1"},
		{Role: "tool", Content: "resultado 2"},
		{Role: "assistant", Content: "Pronto."},
	}

	got := RenderPrompt(messages, nil, true)
	// Exactly one <|im_start|>user...<|im_end|> turn wraps both responses,
	// and there is no stray blank line between the two </tool_response>/
	// <tool_response> boundaries or before the closing <|im_end|>.
	want := "<|im_start|>user" +
		"\n<tool_response>\nresultado 1\n</tool_response>" +
		"\n<tool_response>\nresultado 2\n</tool_response>" +
		"<|im_end|>\n" +
		"<|im_start|>assistant\nPronto.<|im_end|>\n" +
		"<|im_start|>assistant\n"

	if got != want {
		t.Fatalf("RenderPrompt mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestParseOutput_PlainContent(t *testing.T) {
	content, reasoning, calls := ParseOutput("Olá, tudo bem?")
	if content != "Olá, tudo bem?" || reasoning != "" || len(calls) != 0 {
		t.Fatalf("got content=%q reasoning=%q calls=%v", content, reasoning, calls)
	}
}

func TestParseOutput_ThinkBlockExtracted(t *testing.T) {
	raw := "<think>\nvou calcular\n</think>\n\nA resposta é 4."
	content, reasoning, calls := ParseOutput(raw)
	if reasoning != "vou calcular" {
		t.Fatalf("reasoning = %q, want %q", reasoning, "vou calcular")
	}
	if content != "A resposta é 4." {
		t.Fatalf("content = %q, want %q", content, "A resposta é 4.")
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %v, want none", calls)
	}
}

func TestParseOutput_SingleValidToolCall(t *testing.T) {
	raw := `<tool_call>
{"name": "get_time", "arguments": {"timezone": "Asia/Tokyo"}}
</tool_call>`

	content, _, calls := ParseOutput(raw)
	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "get_time" {
		t.Fatalf("Name = %q, want get_time", calls[0].Name)
	}
	if calls[0].Arguments["timezone"] != "Asia/Tokyo" {
		t.Fatalf("Arguments = %v", calls[0].Arguments)
	}
	if calls[0].ID == "" {
		t.Fatalf("ID must not be empty")
	}
}

func TestParseOutput_MalformedToolCall_FallsBackToRaw(t *testing.T) {
	raw := `<tool_call>{"name": "get_time", "arguments": {oops}}</tool_call>`

	content, _, calls := ParseOutput(raw)
	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Arguments["raw"] == nil {
		t.Fatalf("expected fallback Arguments[\"raw\"], got %v", calls[0].Arguments)
	}
}

func TestParseOutput_DoubleEncodedArguments(t *testing.T) {
	raw := `<tool_call>{"name": "get_time", "arguments": "{\"timezone\": \"Asia/Tokyo\"}"}</tool_call>`

	_, _, calls := ParseOutput(raw)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Arguments["timezone"] != "Asia/Tokyo" {
		t.Fatalf("Arguments = %v", calls[0].Arguments)
	}
}

func TestParseOutput_ContentSurroundingToolCall(t *testing.T) {
	raw := "Deixa eu checar.\n<tool_call>\n" +
		`{"name": "get_time", "arguments": {}}` + "\n</tool_call>\nPronto."

	content, _, calls := ParseOutput(raw)
	if content != "Deixa eu checar.\n\nPronto." {
		t.Fatalf("content = %q", content)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
}

func TestParseOutput_MultipleToolCalls_MultiTurnHistoryRoundTrip(t *testing.T) {
	raw := `<tool_call>{"name": "a", "arguments": {"x": 1}}</tool_call>` +
		`<tool_call>{"name": "b", "arguments": {"y": 2}}</tool_call>`

	_, _, calls := ParseOutput(raw)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Name != "a" || calls[1].Name != "b" {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].ID == calls[1].ID {
		t.Fatalf("expected distinct IDs, got %q twice", calls[0].ID)
	}
}

func TestParseOutput_CapsAtFourToolCalls(t *testing.T) {
	block := `<tool_call>{"name": "f", "arguments": {}}</tool_call>`
	raw := block + block + block + block + block // 5 calls

	_, _, calls := ParseOutput(raw)
	if len(calls) != maxParsedToolCalls {
		t.Fatalf("got %d calls, want %d (cap)", len(calls), maxParsedToolCalls)
	}
}

func TestParseOutput_UnterminatedToolCall_TreatedAsContent(t *testing.T) {
	raw := "Resposta parcial <tool_call>{\"name\": \"x\""

	content, _, calls := ParseOutput(raw)
	if len(calls) != 0 {
		t.Fatalf("got %d calls, want 0 for an unterminated tag", len(calls))
	}
	if content != raw {
		t.Fatalf("content = %q, want the raw text preserved verbatim: %q", content, raw)
	}
}
