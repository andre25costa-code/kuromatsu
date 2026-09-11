// Package localllm implements an in-process, cgo-backed LLM provider that
// runs a local GGUF model (Bonsai-1.7B-Q1_0) directly inside the kuromatsu
// binary, without a server or network hop. See software-spec/03-system-design/18-ai-component.md
// and ADR-001/002/003/004 for the rationale.
package localllm

import "context"

// Options configures a single model instance. Populated from
// config.ModelConfig.ExtraBody by the providers factory (E6); zero-value
// fields fall back to the defaults below via WithDefaults.
type Options struct {
	// ModelPath is the resolved absolute path to the GGUF file.
	ModelPath string

	NCtx          int
	NThreads      int
	NBatch        int
	KVCacheType   string // "q8_0" (default) | "f16"
	KeepAliveSecs int    // 0 = never unload
	MaxPredict    int

	Temperature float32
	TopK        int32
	TopP        float32
}

// WithDefaults returns a copy of o with zero-value fields replaced by the
// Bonsai-1.7B baked sampler defaults and the 1GB-RAM-safe runtime defaults
// (ADR-003): n_ctx=2048, kv_cache_type=q8_0, keep_alive=300s.
func (o Options) WithDefaults() Options {
	if o.NCtx <= 0 {
		o.NCtx = 2048
	}
	if o.NThreads <= 0 {
		o.NThreads = 4
	}
	if o.NBatch <= 0 {
		o.NBatch = 256
	}
	if o.KVCacheType == "" {
		o.KVCacheType = "q8_0"
	}
	if o.KeepAliveSecs == 0 {
		o.KeepAliveSecs = 300
	}
	if o.MaxPredict <= 0 {
		o.MaxPredict = 1024
	}
	if o.Temperature == 0 {
		o.Temperature = 0.5
	}
	if o.TopK == 0 {
		o.TopK = 20
	}
	if o.TopP == 0 {
		o.TopP = 0.85
	}
	return o
}

// CompletionResult is the raw output of one engine.completion call, before
// ChatML parsing splits it into content/reasoning/tool calls.
type CompletionResult struct {
	Text         string
	PromptTokens int
	OutputTokens int
	StoppedByEOG bool // true if generation stopped on an end-of-generation token, false if MaxPredict was hit
}

// engine is the seam between the provider adapter (provider.go) and the
// backend implementation, which is either the real cgo binding
// (engine_cgo.go, build tag nativellm&&cgo) or a stub that always returns
// ErrNotBuilt (engine_stub.go, the complementary tag). Exactly one of the
// two files compiles for any given build.
type engine interface {
	completion(ctx context.Context, prompt string, opts Options) (CompletionResult, error)
	unload()
}

// Built reports whether this binary was compiled with the nativellm cgo
// engine. The stub build returns false; used by status/diagnostic commands
// (FR-004) and by config.ApplyNativeFallback (FR-003) to avoid wiring a
// dead candidate into the fallback chain.
func Built() bool {
	return built
}
