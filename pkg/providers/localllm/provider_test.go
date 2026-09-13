package localllm

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/protocoltypes"
)

// fakeEngine lets provider_test.go exercise Provider.Chat without a real
// llama context. The genuine single-flight guarantee (AC-001-4) lives
// inside engine_cgo.go's mutex (E5) and is tested there; this fake has no
// locking of its own.
type fakeEngine struct {
	result      CompletionResult
	err         error
	calls       int
	lastPrompt  string
	lastOpts    Options
	unloadCalls int
}

func (f *fakeEngine) completion(_ context.Context, prompt string, opts Options) (CompletionResult, error) {
	f.calls++
	f.lastPrompt = prompt
	f.lastOpts = opts
	return f.result, f.err
}

func (f *fakeEngine) unload() { f.unloadCalls++ }

func TestNewProvider_RequiresModelPath(t *testing.T) {
	_, err := NewProvider(Options{})
	if err == nil {
		t.Fatal("expected an error for empty ModelPath")
	}
}

func TestNewProvider_SharesInstanceByPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.gguf")

	p1, err := NewProvider(Options{ModelPath: path})
	if err != nil {
		t.Fatalf("NewProvider #1: %v", err)
	}
	p2, err := NewProvider(Options{ModelPath: path})
	if err != nil {
		t.Fatalf("NewProvider #2: %v", err)
	}
	if p1 != p2 {
		t.Fatal("expected the same *Provider instance for the same ModelPath (ADR-003 single context per model)")
	}
}

func TestNewProvider_DistinctPathsGetDistinctInstances(t *testing.T) {
	p1, _ := NewProvider(Options{ModelPath: filepath.Join(t.TempDir(), "a.gguf")})
	p2, _ := NewProvider(Options{ModelPath: filepath.Join(t.TempDir(), "b.gguf")})
	if p1 == p2 {
		t.Fatal("expected distinct instances for distinct ModelPath values")
	}
}

func TestProvider_GetDefaultModel_DerivedFromFilename(t *testing.T) {
	p := &Provider{opts: Options{ModelPath: "/models/Bonsai-1.7B-Q1_0.gguf"}}
	if got := p.GetDefaultModel(); got != "Bonsai-1.7B-Q1_0" {
		t.Fatalf("GetDefaultModel() = %q, want Bonsai-1.7B-Q1_0", got)
	}
}

func TestProvider_Chat_ParsesToolCallFromCompletion(t *testing.T) {
	fake := &fakeEngine{result: CompletionResult{
		Text: `<tool_call>{"name": "get_time", "arguments": {"timezone": "Asia/Tokyo"}}</tool_call>`,
	}}
	p := &Provider{opts: Options{}.WithDefaults(), eng: fake}

	resp, err := p.Chat(context.Background(),
		[]protocoltypes.Message{{Role: "user", Content: "que horas são?"}},
		nil, "bonsai-local", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_time" {
		t.Fatalf("ToolCalls = %+v", resp.ToolCalls)
	}
	if fake.calls != 1 {
		t.Fatalf("engine.completion called %d times, want 1", fake.calls)
	}
}

func TestProvider_Chat_PlainContent_StopFinishReason(t *testing.T) {
	fake := &fakeEngine{result: CompletionResult{Text: "Olá! Tudo bem."}}
	p := &Provider{opts: Options{}.WithDefaults(), eng: fake}

	resp, err := p.Chat(context.Background(),
		[]protocoltypes.Message{{Role: "user", Content: "oi"}}, nil, "bonsai-local", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Content != "Olá! Tudo bem." {
		t.Fatalf("Content = %q", resp.Content)
	}
}

func TestProvider_Chat_PropagatesEngineError(t *testing.T) {
	fake := &fakeEngine{err: ErrContextOverflow}
	p := &Provider{opts: Options{}.WithDefaults(), eng: fake}

	_, err := p.Chat(context.Background(),
		[]protocoltypes.Message{{Role: "user", Content: "oi"}}, nil, "bonsai-local", nil)
	if err != ErrContextOverflow {
		t.Fatalf("Chat() error = %v, want ErrContextOverflow", err)
	}
}

func TestProvider_Chat_OptionsOverrideMaxTokensAndTemperature(t *testing.T) {
	fake := &fakeEngine{result: CompletionResult{Text: "ok"}}
	p := &Provider{opts: Options{MaxPredict: 1024, Temperature: 0.5}, eng: fake}

	_, err := p.Chat(context.Background(),
		[]protocoltypes.Message{{Role: "user", Content: "oi"}}, nil, "bonsai-local",
		map[string]any{"max_tokens": 64, "temperature": 0.1})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if fake.lastOpts.MaxPredict != 64 {
		t.Fatalf("MaxPredict = %d, want 64 (overridden by options)", fake.lastOpts.MaxPredict)
	}
	if fake.lastOpts.Temperature != float32(0.1) {
		t.Fatalf("Temperature = %v, want 0.1 (overridden by options)", fake.lastOpts.Temperature)
	}
}

func TestProvider_Chat_ZeroOrMissingOptions_KeepsConfiguredDefaults(t *testing.T) {
	fake := &fakeEngine{result: CompletionResult{Text: "ok"}}
	p := &Provider{opts: Options{MaxPredict: 777, Temperature: 0.5}, eng: fake}

	_, err := p.Chat(context.Background(),
		[]protocoltypes.Message{{Role: "user", Content: "oi"}}, nil, "bonsai-local", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if fake.lastOpts.MaxPredict != 777 {
		t.Fatalf("MaxPredict = %d, want unchanged 777", fake.lastOpts.MaxPredict)
	}
}

// TestUnloadAll_CallsUnloadOnEveryRegisteredProvider covers ADR-017/C3:
// memguard's PSI watchdog calls UnloadAll under sustained pressure and
// expects every process-wide cached engine to actually unload. Manipulates
// the package-level registry directly (same package) with a fake engine,
// rather than going through NewProvider (which always constructs the real
// engine via newEngine(), never a test double) -- unrelated entries other
// tests may have left in the shared registry are untouched by this
// assertion (UnloadAll calling unload() on them too is harmless, see the
// no-panic test below).
func TestUnloadAll_CallsUnloadOnEveryRegisteredProvider(t *testing.T) {
	fake := &fakeEngine{}
	path := filepath.Join(t.TempDir(), "unload-all-test.gguf")

	registryMu.Lock()
	registry[path] = &Provider{opts: Options{ModelPath: path}, eng: fake}
	registryMu.Unlock()

	UnloadAll()

	if fake.unloadCalls != 1 {
		t.Fatalf("unloadCalls = %d, want 1", fake.unloadCalls)
	}
}

func TestUnloadAll_SafeRegardlessOfRegistryState(t *testing.T) {
	// Not asserting an empty registry (other tests populate it with real/
	// stub engines that were never actually loaded) -- just that calling
	// UnloadAll never panics no matter what's in there.
	UnloadAll()
}

func TestProvider_Chat_RendersToolsIntoPrompt(t *testing.T) {
	fake := &fakeEngine{result: CompletionResult{Text: "ok"}}
	p := &Provider{opts: Options{}.WithDefaults(), eng: fake}

	tools := []protocoltypes.ToolDefinition{
		{Type: "function", Function: protocoltypes.ToolFunctionDefinition{Name: "get_time"}},
	}
	_, err := p.Chat(context.Background(),
		[]protocoltypes.Message{{Role: "user", Content: "oi"}}, tools, "bonsai-local", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	for _, want := range []string{"<tools>", "get_time", "<tool_call>"} {
		if !strings.Contains(fake.lastPrompt, want) {
			t.Fatalf("expected the rendered prompt to contain %q, got: %q", want, fake.lastPrompt)
		}
	}
}
