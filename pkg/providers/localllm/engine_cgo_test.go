//go:build nativellm && cgo

package localllm

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/protocoltypes"
)

// TestCgoEngine_RealModel_Integration exercises the real cgo engine against
// the actual Bonsai GGUF. Opt-in (like pkg/updater's release-download
// test): needs the model on disk and a full `make build-native` toolchain,
// neither of which CI or a plain `go test ./...` should require.
// Enable with: KUROMATSU_INTEGRATION_TESTS=1 KUROMATSU_TEST_MODEL=<path>
func TestCgoEngine_RealModel_Integration(t *testing.T) {
	if os.Getenv("KUROMATSU_INTEGRATION_TESTS") == "" {
		t.Skip("skipping integration test (set KUROMATSU_INTEGRATION_TESTS=1 to enable)")
	}
	modelPath := os.Getenv("KUROMATSU_TEST_MODEL")
	if modelPath == "" {
		modelPath = "../../../models/Bonsai-1.7B-Q1_0.gguf"
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("model not found at %s: %v", modelPath, err)
	}

	provider, err := NewProvider(Options{ModelPath: modelPath, NCtx: 512, MaxPredict: 32})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	resp, err := provider.Chat(context.Background(),
		[]protocoltypes.Message{
			{Role: "system", Content: "Responda em português, de forma direta."},
			{Role: "user", Content: "Diga olá em uma frase curta."},
		},
		nil, provider.GetDefaultModel(), nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Fatal("expected non-empty content from a real completion")
	}
	if resp.Usage == nil || resp.Usage.CompletionTokens == 0 {
		t.Fatalf("expected non-zero completion tokens, got usage=%+v", resp.Usage)
	}
	t.Logf("response: %q (prompt_tok=%d output_tok=%d)", resp.Content, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
}

// TestCgoEngine_ContextOverflow_Integration confirms an oversized prompt is
// classified as a non-retriable context overflow rather than crashing.
func TestCgoEngine_ContextOverflow_Integration(t *testing.T) {
	if os.Getenv("KUROMATSU_INTEGRATION_TESTS") == "" {
		t.Skip("skipping integration test (set KUROMATSU_INTEGRATION_TESTS=1 to enable)")
	}
	modelPath := os.Getenv("KUROMATSU_TEST_MODEL")
	if modelPath == "" {
		modelPath = "../../../models/Bonsai-1.7B-Q1_0.gguf"
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("model not found at %s: %v", modelPath, err)
	}

	provider, err := NewProvider(Options{ModelPath: modelPath, NCtx: 32, MaxPredict: 8})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	longContent := strings.Repeat("palavra ", 500)
	_, err = provider.Chat(context.Background(),
		[]protocoltypes.Message{{Role: "user", Content: longContent}},
		nil, provider.GetDefaultModel(), nil)
	if err != ErrContextOverflow {
		t.Fatalf("err = %v, want ErrContextOverflow", err)
	}
}
