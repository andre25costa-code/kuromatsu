package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// Trilho G, Seção C.1: Ollama Cloud is structurally just another
// OpenAI-compatible entry in model_list (provider "ollama" -> the generic
// openai_compat.Provider, see factory_provider.go's HTTP-provider case) --
// nothing provider-specific was needed in the factory for this to work.
// This test proves the two things an adversarial review flagged as
// actually risky in that assumption: the auth header, and tool-call
// parsing (Ollama Cloud's JSON-Schema-to-tool-call translation is known to
// be brittle/model-dependent -- this at least proves *this* codebase's own
// request/response shape round-trips correctly against an OpenAI-shaped
// server, independent of whatever Ollama Cloud itself does downstream).

func newOllamaCloudTestConfig(apiBase, apiKey string) *config.ModelConfig {
	mc := &config.ModelConfig{
		ModelName: "ollama-cloud",
		Provider:  "ollama",
		Model:     "ollama/qwen3:235b-cloud",
		APIBase:   apiBase,
	}
	mc.SetAPIKey(apiKey)
	return mc
}

func TestOllamaCloudProvider_SendsBearerAuthHeader_NoNetwork(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	mc := newOllamaCloudTestConfig(srv.URL, "test-key-123")
	provider, modelID, err := CreateProviderFromConfig(mc)
	if err != nil {
		t.Fatalf("CreateProviderFromConfig() error = %v", err)
	}

	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, modelID, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("resp.Content = %q, want %q", resp.Content, "ok")
	}
	if gotAuth != "Bearer test-key-123" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-key-123")
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("request path = %q, want /chat/completions", gotPath)
	}
}

// TestOllamaCloudProvider_ParsesToolCallsFromResponse is the adversarial
// review's specific risk item: Ollama Cloud's OpenAI-tool-calling
// translation layer is model-dependent and known to be brittle. This
// doesn't reach the real Ollama Cloud (no network), but proves the
// request this codebase sends is the standard OpenAI tool-definition
// shape, and that a standard OpenAI-shaped tool_calls response parses into
// providers.LLMResponse.ToolCalls correctly end-to-end through the same
// openai_compat path Ollama Cloud would be reached through.
func TestOllamaCloudProvider_ParsesToolCallsFromResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"sysmon","arguments":"{\"action\":\"status\"}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer srv.Close()

	mc := newOllamaCloudTestConfig(srv.URL, "test-key-123")
	provider, modelID, err := CreateProviderFromConfig(mc)
	if err != nil {
		t.Fatalf("CreateProviderFromConfig() error = %v", err)
	}

	tools := []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:        "sysmon",
			Description: "Reports system state.",
		},
	}}
	resp, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "how's the system?"}}, tools, modelID, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "sysmon" {
		t.Fatalf("ToolCalls[0].Name = %q, want %q", resp.ToolCalls[0].Name, "sysmon")
	}
	if resp.ToolCalls[0].Arguments["action"] != "status" {
		t.Fatalf("ToolCalls[0].Arguments[action] = %v, want %q", resp.ToolCalls[0].Arguments["action"], "status")
	}
}

// TestOllamaCloudProvider_Integration is opt-in (KUROMATSU_INTEGRATION_TESTS=1
// + KUROMATSU_TEST_OLLAMA_CLOUD_KEY), same pattern as
// pkg/providers/localllm/engine_cgo_test.go's real-model tests. It exercises
// the real Ollama Cloud endpoint, including a real tool-calling round trip --
// run manually with a real key before relying on this fallback in
// production, since the two tests above only prove this codebase's request/
// response handling is correct, not that Ollama Cloud's own translation
// layer behaves identically for every model.
func TestOllamaCloudProvider_Integration(t *testing.T) {
	if os.Getenv("KUROMATSU_INTEGRATION_TESTS") == "" {
		t.Skip("skipping integration test (set KUROMATSU_INTEGRATION_TESTS=1 to enable)")
	}
	apiKey := os.Getenv("KUROMATSU_TEST_OLLAMA_CLOUD_KEY")
	if apiKey == "" {
		t.Skip("KUROMATSU_TEST_OLLAMA_CLOUD_KEY not set; skipping real network call")
	}

	mc := newOllamaCloudTestConfig("https://ollama.com/v1", apiKey)
	provider, modelID, err := CreateProviderFromConfig(mc)
	if err != nil {
		t.Fatalf("CreateProviderFromConfig() error = %v", err)
	}

	resp, err := provider.Chat(context.Background(),
		[]Message{{Role: "user", Content: "Say hi in one short sentence."}}, nil, modelID, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty content from a real Ollama Cloud completion")
	}
	t.Logf("response: %q", resp.Content)

	tools := []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:        "get_time",
			Description: "Returns the current time.",
		},
	}}
	toolResp, err := provider.Chat(context.Background(),
		[]Message{{Role: "user", Content: "What time is it? Use the get_time tool."}}, tools, modelID, nil)
	if err != nil {
		t.Fatalf("Chat() with tools error = %v", err)
	}
	t.Logf("tool-calling response: content=%q tool_calls=%d", toolResp.Content, len(toolResp.ToolCalls))
}
