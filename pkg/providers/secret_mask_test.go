package providers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// captureProvider records what actually leaves for the provider and
// replies with a scripted response.
type captureProvider struct {
	sent     []Message
	response *LLMResponse
}

func (c *captureProvider) Chat(
	_ context.Context, messages []Message, _ []ToolDefinition, _ string, _ map[string]any,
) (*LLMResponse, error) {
	c.sent = messages
	if c.response == nil {
		return &LLMResponse{Content: "ok"}, nil
	}
	return c.response, nil
}

func (c *captureProvider) GetDefaultModel() string { return "capture" }

const testKey = "sk-proj-abcdefghijklmnop1234"

// AC-029-1: credentials never leave the VM in clear text for an external
// provider -- in user content, system blocks or past tool-call arguments.
func TestSecretMask_OutboundCredentialsReplaced(t *testing.T) {
	delegate := &captureProvider{}
	p := wrapProviderWithSecretMask(delegate)

	_, err := p.Chat(context.Background(), []Message{
		{Role: "system", Content: "sys", SystemParts: []ContentBlock{{Type: "text", Text: "token=" + testKey}}},
		{Role: "user", Content: "minha chave é " + testKey + " e o header é Bearer abcdefghijklmnopqrstuvwx"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "1", Function: &FunctionCall{Name: "exec", Arguments: `{"env":"` + testKey + `"}`}},
			},
		},
	}, nil, "m", nil)
	if err != nil {
		t.Fatal(err)
	}

	var sent strings.Builder
	for _, m := range delegate.sent {
		sent.WriteString(m.Content)
		for _, part := range m.SystemParts {
			sent.WriteString(part.Text)
		}
		for _, tc := range m.ToolCalls {
			sent.WriteString(tc.Function.Arguments)
		}
	}
	if strings.Contains(sent.String(), testKey) || strings.Contains(sent.String(), "abcdefghijklmnopqrstuvwx") {
		t.Fatalf("credential left in clear text: %q", sent.String())
	}
	if !strings.Contains(sent.String(), "[SECRET_1]") || !strings.Contains(sent.String(), "Bearer [SECRET_") {
		t.Fatalf("placeholders missing (Bearer prefix must be kept): %q", sent.String())
	}
}

// AC-029-2: placeholders the model echoes back are restored locally, in the
// text and in tool-call arguments, so tools receive the real value.
func TestSecretMask_ResponsePlaceholdersRestored(t *testing.T) {
	delegate := &captureProvider{response: &LLMResponse{
		Content: "vou usar [SECRET_1]",
		ToolCalls: []ToolCall{{
			ID:        "1",
			Function:  &FunctionCall{Name: "exec", Arguments: `{"cmd":"echo [SECRET_1]"}`},
			Arguments: map[string]any{"cmd": "echo [SECRET_1]"},
		}},
	}}
	p := wrapProviderWithSecretMask(delegate)

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "use " + testKey}}, nil, "m", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "vou usar "+testKey {
		t.Fatalf("content = %q, want the real key restored", resp.Content)
	}
	if !strings.Contains(resp.ToolCalls[0].Function.Arguments, testKey) ||
		resp.ToolCalls[0].Arguments["cmd"] != "echo "+testKey {
		t.Fatalf("tool call arguments not restored: %+v / %+v", resp.ToolCalls[0].Function, resp.ToolCalls[0].Arguments)
	}
}

// The caller's history is never rewritten: masking works on copies.
func TestSecretMask_CallerMessagesUntouched(t *testing.T) {
	p := wrapProviderWithSecretMask(&captureProvider{})
	history := []Message{{Role: "user", Content: "key " + testKey}}

	if _, err := p.Chat(context.Background(), history, nil, "m", nil); err != nil {
		t.Fatal(err)
	}
	if history[0].Content != "key "+testKey {
		t.Fatalf("caller history mutated: %q", history[0].Content)
	}
}

// AC-029-5: personal data that is not a credential goes as is.
func TestSecretMask_PersonalDataNotMasked(t *testing.T) {
	delegate := &captureProvider{}
	p := wrapProviderWithSecretMask(delegate)
	msg := "lembrar de mandar flores para a Duda no aniversário, 12/03"

	if _, err := p.Chat(context.Background(), []Message{{Role: "user", Content: msg}}, nil, "m", nil); err != nil {
		t.Fatal(err)
	}
	if delegate.sent[0].Content != msg {
		t.Fatalf("non-credential content changed: %q", delegate.sent[0].Content)
	}
}

// AC-029-3: the native provider is never wrapped (data never leaves the
// VM); every other protocol is.
func TestSecretMask_OnlyExternalProtocolsMasked(t *testing.T) {
	if shouldMaskProtocol("native") {
		t.Fatal("native protocol must not be masked")
	}
	for _, protocol := range []string{"openai", "anthropic", "ollama", "openrouter"} {
		if !shouldMaskProtocol(protocol) {
			t.Fatalf("protocol %q must be masked", protocol)
		}
	}

	cfg := &config.ModelConfig{Model: "openai/gpt-test", APIBase: "http://127.0.0.1:1/v1"}
	cfg.SetAPIKey("k")
	p, _, err := CreateProviderFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	switch p.(type) {
	case *secretMaskProvider, *secretMaskStreamingProvider:
	default:
		t.Fatalf("CreateProviderFromConfig(openai) = %T, want the secret-mask wrapper", p)
	}
}

// unwrapSecretMask lets factory tests keep asserting which concrete
// implementation was selected now that CreateProviderFromConfig wraps
// external providers (FR-029).
func unwrapSecretMask(p LLMProvider) LLMProvider {
	switch w := p.(type) {
	case *secretMaskProvider:
		return w.delegate
	case *secretMaskStreamingProvider:
		return w.delegate
	}
	return p
}

// The anthropic, anthropic_messages and bedrock adapters serialize past tool
// calls from the Arguments map, not Function.Arguments: the map must be
// masked too (on a copy -- the caller's map stays intact).
func TestSecretMask_OutboundToolCallArgumentsMapMasked(t *testing.T) {
	delegate := &captureProvider{}
	p := wrapProviderWithSecretMask(delegate)
	args := map[string]any{"env": map[string]any{"OPENAI_KEY": testKey}, "list": []any{"x " + testKey}}

	_, err := p.Chat(context.Background(), []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Name: "exec", Arguments: args}}},
	}, nil, "m", nil)
	if err != nil {
		t.Fatal(err)
	}

	sent := fmt.Sprint(delegate.sent[0].ToolCalls[0].Arguments)
	if strings.Contains(sent, testKey) {
		t.Fatalf("credential left in the outbound Arguments map: %s", sent)
	}
	if fmt.Sprint(args) == sent || !strings.Contains(fmt.Sprint(args), testKey) {
		t.Fatalf("caller's Arguments map was mutated: %v", args)
	}
}
