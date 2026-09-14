// Package localllm implements an in-process, cgo-backed LLM provider that
// runs a local GGUF model (Bonsai-1.7B-Q1_0) directly inside the kuromatsu
// binary, without a server or network hop. See software-spec/03-system-design/18-ai-component.md
// and ADR-001/002/003/004 for the rationale.
package localllm

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"
)

// Options configures a single model instance. Populated from
// config.ModelConfig.ExtraBody by the providers factory (E6); zero-value
// fields fall back to the defaults below via WithDefaults.
type Options struct {
	// ModelPath is the resolved absolute path to the GGUF file.
	ModelPath string

	NCtx     int
	NThreads int
	// NBatch is the physical compute chunk size (llama.cpp's n_ubatch,
	// which bounds the compute-buffer memory reservation -- see S18/S29).
	// The engine always tells llama.cpp to accept a full n_ctx worth of
	// tokens per logical llama_decode call (n_batch=n_ctx), since prompts
	// are submitted as a single batch with no manual chunking loop.
	NBatch      int
	KVCacheType string // "q8_0" (default) | "f16"
	// KeepAliveSecs is how long the model stays loaded, counted from the
	// END of a completion call (ADR-015 point 4), before being unloaded to
	// free RAM. 0 falls back to the 300s default (WithDefaults); a
	// negative value means "never unload automatically" -- the model then
	// only unloads via an explicit unload()/UnloadAll() call (AC-016-9).
	KeepAliveSecs int
	MaxPredict    int

	// CoreCacheParking enables B2 (window-core KV parking, ADR-015 point 8):
	// when true, the engine keeps up to coreCacheSlots (2, see engine_cgo.go
	// -- fixed, not configurable: the chat+heartbeat pair is the only one
	// that alternates with guaranteed frequency, per the architecture plan's
	// measured rationale) focus-window "cores" (system prompt + tool
	// schemas) resident in the KV cache as extra llama.cpp sequences, so
	// switching between them reuses the already-decoded core instead of
	// paying its prefill again. false (default) preserves pre-B2 behavior
	// exactly: a single sequence, no extra n_seq_max/kv_unified cost.
	// Changing this requires a context reload (it affects how the context
	// itself is created), hence its place in loadKey below rather than
	// being treated like a sampler setting.
	CoreCacheParking bool

	Temperature float32
	TopK        int32
	TopP        float32
}

// WithDefaults returns a copy of o with zero-value fields replaced by the
// Bonsai-1.7B baked sampler defaults and the 1GB-RAM-safe runtime defaults
// (ADR-003/ADR-015): n_ctx=2048, kv_cache_type=q8_0, keep_alive=300s,
// n_threads=min(NumCPU,4).
func (o Options) WithDefaults() Options {
	if o.NCtx <= 0 {
		o.NCtx = 2048
	}
	if o.NThreads <= 0 {
		// ADR-015 point 6: use up to 4 threads, but never more than the
		// hardware actually offers -- the demetrius deploy target (S39)
		// has 1 physical core / 2 HT threads, where n_threads=4 would
		// oversubscribe.
		o.NThreads = min(runtime.NumCPU(), 4)
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

// loadKey is the subset of Options that requires a full model/context
// reload when it changes (AC-016-4, ADR-015 point 7). Sampler settings
// (Temperature/TopK/TopP), MaxPredict and KeepAliveSecs do not affect the
// loaded llama_context -- comparing the full Options value (as v1 did)
// forced a reload on every call whose sampler settings differed, e.g. every
// summarization call, which runs with a lower temperature than normal
// chat turns.
type loadKey struct {
	ModelPath        string
	NCtx             int
	NThreads         int
	NBatch           int
	KVCacheType      string
	CoreCacheParking bool
}

// loadKey extracts the load-affecting fields of o. See the loadKey type
// doc for why sampler/output fields are deliberately excluded.
func (o Options) loadKey() loadKey {
	return loadKey{
		ModelPath:        o.ModelPath,
		NCtx:             o.NCtx,
		NThreads:         o.NThreads,
		NBatch:           o.NBatch,
		KVCacheType:      o.KVCacheType,
		CoreCacheParking: o.CoreCacheParking,
	}
}

// CompletionResult is the raw output of one engine.completion call, before
// ChatML parsing splits it into content/reasoning/tool calls.
type CompletionResult struct {
	Text         string
	PromptTokens int
	OutputTokens int
	StoppedByEOG bool // true if generation stopped on an end-of-generation token, false if MaxPredict was hit

	// CachedTokens is how much of PromptTokens was already resident in the
	// KV cache and therefore skipped re-decoding (B1/ADR-015/S21). 0 means
	// a full miss (cold KV cache, or the new prompt shares no prefix with
	// what was cached).
	CachedTokens int
	// Prefill is how long the initial prompt decode (the PromptTokens-
	// CachedTokens delta) took. Generation is the time spent sampling and
	// decoding output tokens afterwards. Both are zero-value if no decode
	// ran (e.g. the call failed before tokenization completed).
	Prefill    time.Duration
	Generation time.Duration
}

// engine is the seam between the provider adapter (provider.go) and the
// backend implementation, which is either the real cgo binding
// (engine_cgo.go, build tag nativellm&&cgo) or a stub that always returns
// ErrNotBuilt (engine_stub.go, the complementary tag). Exactly one of the
// two files compiles for any given build.
type engine interface {
	// coreEnd is the byte offset in prompt right after the system block
	// (identity + tool schemas), or 0 if there is none -- see
	// RenderPromptParts. Only meaningful when opts.CoreCacheParking is true
	// (B2); ignored otherwise.
	completion(ctx context.Context, prompt string, coreEnd int, opts Options) (CompletionResult, error)
	unload()
}

// Built reports whether this binary was compiled with the nativellm cgo
// engine. The stub build returns false; used by status/diagnostic commands
// (FR-004) and by config.ApplyNativeFallback (FR-003) to avoid wiring a
// dead candidate into the fallback chain.
func Built() bool {
	return built
}

// MemoryEstimate summarizes the RAM one model load is expected to need, in
// bytes (ADR-017/FR-018/C3). Deliberately plain data, not
// runstate.GuardConfig or any other Trilho C type: this package must never
// import pkg/runstate (that would point the dependency the wrong way --
// runstate already depends on nothing upstream of it, per its own leaf-
// package doc comment). The memguard wiring layer (gateway.go) is what
// imports both packages and translates between them.
type MemoryEstimate struct {
	ModelBytes   uint64
	KVBytes      uint64
	ComputeBytes uint64
	NCtx         int
	KVType       string
}

// kvBytesPerTokenQ8_0/F16 are the qwen3-1.7B (Bonsai) per-token KV cache
// costs the plan measured (ADR-017's memguard estimate section). Used only
// to build the MemoryEstimate engine_cgo.go's ensureLoaded passes to the
// pre/postLoad hooks -- deliberately not refined further with
// llama_model_n_layer/n_head_kv/n_embd cgo introspection (a possible
// future follow-up the ADR itself flags as optional): this keeps the C3
// addition to a handful of pure-Go lines with zero new cgo calls, on top
// of engine_cgo.go's already-reviewed cgo surface. Kept in this
// build-tag-free file (not engine_cgo.go) specifically so it can be unit
// tested without a cgo toolchain (types_test.go).
const (
	kvBytesPerTokenQ8_0 = 60928
	kvBytesPerTokenF16  = 114688
)

// estimateMemory builds the MemoryEstimate the memguard pre/postLoad hooks
// receive (ADR-017/C3). ModelBytes is the GGUF file's size on disk -- a
// reasonable proxy for its mmap'd footprint (ADR-013 measured the real
// Bonsai-1.7B-Q1_0.gguf at 237MB); 0 if the file can't be stat'd (the load
// itself will fail moments later in ensureLoaded with a clearer error
// either way, so silently estimating 0 here is not a hidden failure).
func estimateMemory(opts Options) MemoryEstimate {
	bytesPerToken := uint64(kvBytesPerTokenQ8_0)
	if opts.KVCacheType == "f16" {
		bytesPerToken = kvBytesPerTokenF16
	}
	var modelBytes uint64
	if info, err := os.Stat(opts.ModelPath); err == nil {
		modelBytes = uint64(info.Size())
	}
	return MemoryEstimate{
		ModelBytes: modelBytes,
		KVBytes:    uint64(opts.NCtx) * bytesPerToken,
		NCtx:       opts.NCtx,
		KVType:     opts.KVCacheType,
	}
}

var (
	memoryHooksMu sync.Mutex
	preLoadHook   func(MemoryEstimate) error
	postLoadHook  func(MemoryEstimate)
)

// SetMemoryHooks installs the memguard preLoad/postLoad callbacks
// (ADR-017): pre is called by ensureLoaded right before a model actually
// loads and can refuse the load (AC-018-2, e.g. insufficient
// MemAvailable) by returning a non-nil error; post is called right after
// a successful load, once ensureLoaded knows the real n_ctx/kv_cache_type
// it loaded with (AC-018-1). Passing nil for either removes it. Both are
// nil until something calls this -- the default, memguard.enabled=false
// (or a platform with no PSI, AC-018-6) path -- so ensureLoaded's calls
// into callPreLoadHook/callPostLoadHook are then unconditional no-ops,
// exactly preserving pre-C3 behavior.
func SetMemoryHooks(pre func(MemoryEstimate) error, post func(MemoryEstimate)) {
	memoryHooksMu.Lock()
	defer memoryHooksMu.Unlock()
	preLoadHook = pre
	postLoadHook = post
}

// callPreLoadHook is ensureLoaded's seam into the installed preLoad hook
// (a plain nil-check, not exported: engine_cgo.go/engine_stub.go are the
// only callers).
func callPreLoadHook(est MemoryEstimate) error {
	memoryHooksMu.Lock()
	hook := preLoadHook
	memoryHooksMu.Unlock()
	if hook == nil {
		return nil
	}
	return hook(est)
}

// callPostLoadHook is ensureLoaded's seam into the installed postLoad hook.
func callPostLoadHook(est MemoryEstimate) {
	memoryHooksMu.Lock()
	hook := postLoadHook
	memoryHooksMu.Unlock()
	if hook != nil {
		hook(est)
	}
}
