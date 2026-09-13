//go:build nativellm && cgo

package localllm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/protocoltypes"
)

// integrationModelPath resolves the GGUF path shared by every opt-in
// integration test below, honoring KUROMATSU_TEST_MODEL like the existing
// tests in this file. Returns "" (with the test already skipped) if the
// integration tests aren't enabled or the model isn't on disk.
func integrationModelPath(t *testing.T) string {
	t.Helper()
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
	return modelPath
}

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

// TestCgoEngine_ReloadGuard_Integration is AC-016-4: calls whose Options
// only differ in MaxPredict/Temperature (sampler/output fields, excluded
// from loadKey -- see types.go) must not force a model/context reload.
func TestCgoEngine_ReloadGuard_Integration(t *testing.T) {
	modelPath := integrationModelPath(t)

	// Construct the Provider directly instead of going through NewProvider:
	// the registry it maintains is keyed only by ModelPath (ADR-002/003, "one
	// Provider per model path"), so every integration test in this file that
	// resolves the same modelPath would otherwise share one *cgoEngine -- the
	// first NewProvider() call to run wins, and every other test's Options
	// (including NCtx) are silently ignored. A private *Provider gives this
	// test its own *cgoEngine, so its own loadCount baseline is meaningful.
	provider := &Provider{opts: Options{ModelPath: modelPath, NCtx: 384, MaxPredict: 16}.WithDefaults(), eng: newEngine()}
	eng, ok := provider.eng.(*cgoEngine)
	if !ok {
		t.Fatalf("provider.eng is %T, want *cgoEngine", provider.eng)
	}

	messages := []protocoltypes.Message{
		{Role: "system", Content: "Responda em português, de forma direta."},
		{Role: "user", Content: "Diga olá em uma frase curta."},
	}

	// Warm-up call: establishes this test's own loadKey (and, incidentally,
	// whatever loadCount was before this belongs to earlier tests -- the
	// baseline below is captured *after* this call, so it's irrelevant).
	if _, err := provider.Chat(context.Background(), messages, nil, provider.GetDefaultModel(), nil); err != nil {
		t.Fatalf("warm-up Chat() error = %v", err)
	}
	baseline := eng.loadCount

	// Two more calls with the same n_ctx/n_threads/n_batch/kv_cache_type
	// but different max_tokens/temperature -- e.g. what a summarization
	// call uses relative to a normal chat turn. Neither should reload.
	if _, err := provider.Chat(context.Background(), messages, nil, provider.GetDefaultModel(),
		map[string]any{"max_tokens": 8, "temperature": 0.3}); err != nil {
		t.Fatalf("2nd Chat() error = %v", err)
	}
	if _, err := provider.Chat(context.Background(), messages, nil, provider.GetDefaultModel(),
		map[string]any{"max_tokens": 32, "temperature": 0.9}); err != nil {
		t.Fatalf("3rd Chat() error = %v", err)
	}

	if eng.loadCount != baseline {
		t.Fatalf("loadCount changed from %d to %d across calls that only vary sampler/max_tokens (AC-016-4)", baseline, eng.loadCount)
	}
}

// TestCgoEngine_AbortMidPrefill_Integration is AC-016-5: cancelling the
// context ~200ms into a long prefill must unwind the llama_decode call
// itself via abort_callback (not just be noticed between whole decode
// calls), and do so quickly rather than after the full prefill finishes.
func TestCgoEngine_AbortMidPrefill_Integration(t *testing.T) {
	modelPath := integrationModelPath(t)

	// Direct construction (see the comment in TestCgoEngine_ReloadGuard_Integration
	// for why NewProvider()'s shared, ModelPath-only-keyed registry would silently
	// ignore this NCtx if another integration test already resolved it first): this
	// test needs its own *cgoEngine so the 4096 below is the real, effective n_ctx.
	provider := &Provider{opts: Options{ModelPath: modelPath, NCtx: 4096, MaxPredict: 64}.WithDefaults(), eng: newEngine()}

	// ~1500+ tokens once tokenized -- long enough that, on hardware
	// without the AVX-512-VNNI Q1_0 repack kernel (S18/ADR-015: neither
	// the Oracle nor the demetrius hosts have it), a full prefill would
	// take far longer than the 3s budget below if the abort didn't work.
	longUser := strings.Repeat("palavra ", 2000)
	messages := []protocoltypes.Message{
		{Role: "system", Content: "Responda em português, de forma direta."},
		{Role: "user", Content: longUser},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	_, err := provider.Chat(ctx, messages, nil, provider.GetDefaultModel(), nil)
	elapsed := time.Since(start)

	if err != context.Canceled {
		t.Fatalf("Chat() error = %v, want context.Canceled", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Chat() took %s to return after cancellation, want < 3s (AC-016-5)", elapsed)
	}
}

// TestCgoEngine_PrefixCache_Integration is B1/AC-016-1/AC-016-2: a cold
// call, a partial hit (same system prompt, new user turn), and a full hit
// (an exact repeat of the previous call).
func TestCgoEngine_PrefixCache_Integration(t *testing.T) {
	modelPath := integrationModelPath(t)

	// Direct construction, own *cgoEngine (see the comment in
	// TestCgoEngine_ReloadGuard_Integration): the first call below asserts
	// CachedTokens==0, which requires a genuinely cold, empty KV cache --
	// NewProvider()'s shared registry would silently reuse whatever KV state
	// another integration test already left on the process-wide *cgoEngine.
	provider := &Provider{opts: Options{ModelPath: modelPath, NCtx: 1024, MaxPredict: 16}.WithDefaults(), eng: newEngine()}

	systemMsg := protocoltypes.Message{Role: "system", Content: "Responda sempre em português, de forma direta e objetiva."}

	resp1, err := provider.Chat(context.Background(),
		[]protocoltypes.Message{systemMsg, {Role: "user", Content: "Diga olá em uma frase curta."}},
		nil, provider.GetDefaultModel(), nil)
	if err != nil {
		t.Fatalf("1st Chat() error = %v", err)
	}
	if resp1.Usage.CachedTokens != 0 {
		t.Fatalf("1st call CachedTokens = %d, want 0 (cold KV cache)", resp1.Usage.CachedTokens)
	}

	resp2, err := provider.Chat(context.Background(),
		[]protocoltypes.Message{systemMsg, {Role: "user", Content: "Qual é a capital de Portugal?"}},
		nil, provider.GetDefaultModel(), nil)
	if err != nil {
		t.Fatalf("2nd Chat() error = %v", err)
	}
	if resp2.Usage.CachedTokens <= 0 || resp2.Usage.CachedTokens >= resp2.Usage.PromptTokens {
		t.Fatalf("2nd call CachedTokens = %d, PromptTokens = %d; want 0 < CachedTokens < PromptTokens (AC-016-1)",
			resp2.Usage.CachedTokens, resp2.Usage.PromptTokens)
	}

	resp3, err := provider.Chat(context.Background(),
		[]protocoltypes.Message{systemMsg, {Role: "user", Content: "Qual é a capital de Portugal?"}},
		nil, provider.GetDefaultModel(), nil)
	if err != nil {
		t.Fatalf("3rd Chat() error = %v", err)
	}
	if want := resp3.Usage.PromptTokens - 1; resp3.Usage.CachedTokens != want {
		t.Fatalf("3rd call (identical to 2nd) CachedTokens = %d, want PromptTokens-1 = %d (AC-016-2)",
			resp3.Usage.CachedTokens, want)
	}
}
