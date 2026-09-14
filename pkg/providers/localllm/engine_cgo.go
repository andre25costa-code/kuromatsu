//go:build nativellm && cgo

package localllm

/*
#cgo CFLAGS: -I${SRCDIR}/../../../llama.cpp/include -I${SRCDIR}/../../../llama.cpp/ggml/include
#cgo LDFLAGS: -L${SRCDIR}/../../../llama.cpp/build-native/lib
#cgo LDFLAGS: -lllama -lggml -lggml-cpu -lggml-base -lstdc++ -lm -lpthread
#include <stdlib.h>
#include "llama.h"

// kuromatsu_abort_flag / kuromatsu_abort_cb / kuromatsu_set_abort back the
// abort_callback wired into every loaded llama_context (ADR-015 point 5):
// llama_decode polls the callback periodically during a long-running
// prefill/generation and unwinds early (return code 2) once it flips true,
// so a context cancellation actually interrupts an in-flight decode instead
// of only being noticed between whole llama_decode calls.
//
// This is deliberately plain C, not a Go function exported back to C via
// //export: the callback's C signature (bool(*)(void*), see
// ggml_abort_callback in ggml.h) would otherwise depend on exactly how cgo
// lowers a Go bool across the export boundary. Keeping the callback itself
// in C sidesteps that ABI question entirely: Go only ever flips an int flag
// through the two tiny setters below, a well-established, low-risk cgo
// pattern (calling a plain C function).
//
// A single process-wide flag is correct here because ADR-003's one-context-
// per-process design (S18) means at most one cgoEngine is ever actually
// decoding at a time in this binary; if that assumption ever changes, this
// needs to become one flag per llama_context (e.g. keyed by
// abort_callback_data instead of a bare global).
static volatile int kuromatsu_abort_flag = 0;

// Not static: cgo takes this function's *address* as a value below
// (C.ggml_abort_callback(C.kuromatsu_abort_cb)), which the linker resolves
// from cgo's separately-compiled generated glue code -- a static function
// has internal linkage and is invisible outside this preamble's own
// translation unit, causing "undefined reference to kuromatsu_abort_cb" at
// link time (confirmed for real on the first native CI build after this
// file was written without a compiler available -- see git history).
// kuromatsu_abort_flag/kuromatsu_set_abort below are only ever *called*,
// never referenced by address, so static (internal linkage, inlinable) is
// fine and preferred for them.
bool kuromatsu_abort_cb(void *data) {
	(void)data;
	return kuromatsu_abort_flag != 0;
}

static void kuromatsu_set_abort(int v) {
	kuromatsu_abort_flag = v;
}
*/
import "C"

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// built is true only on a binary compiled with -tags nativellm and
// CGO_ENABLED=1 (see Makefile's build-native target). The stub build
// (engine_stub.go) defines the complementary false value.
const built = true

// contextMargin is reserved headroom (in tokens) so a prompt that exactly
// fills n_ctx still gets flagged as overflow before generation, rather
// than succeeding with zero room to produce output.
const contextMargin = 64

// coreCacheSlots is B2's fixed number of parked-core slots (ADR-015 point
// 8): deliberately not configurable. Only two focus windows alternate with
// guaranteed frequency (chat: the user's own window; heartbeat: fixed
// origin, fires on a timer) -- see the architecture plan's measured
// rationale. Sequence 0 is always the active conversation; parked cores
// live in sequences 1..coreCacheSlots.
const coreCacheSlots = 2

// parkedCore is one B2 slot: a focus window's "core" (system prompt + tool
// schemas -- everything RenderPromptParts' coreEnd covers) resident in its
// own llama.cpp sequence, so switching focus windows can restore it via
// llama_memory_seq_cp instead of re-decoding it. Keyed by a hash of the
// core's rendered bytes, not a window name: any edit to AGENT.md/SOUL.md/
// MEMORY.md or the window's tool catalog changes the hash and therefore
// invalidates automatically, with no separate invalidation bookkeeping.
type parkedCore struct {
	valid bool
	seqID C.llama_seq_id
	hash  [sha256.Size]byte
	nCore int
	// tokens is a copy of the core's decoded tokens, restored into
	// e.kvTokens on a cache hit (mirrors what kvTokens would already
	// contain had this call's prompt been decoded from scratch).
	tokens []C.llama_token
	// gen is a monotonically increasing touch counter (bumped on every
	// park/restore), used to pick the least-recently-used slot to evict
	// once both slots are occupied by a different core.
	gen uint64
}

var backendInitOnce sync.Once

// cgoEngine binds one loaded GGUF model + llama_context. ADR-003: a single
// mutex serializes every call, matching the "one context per process" 1GB-
// RAM design. Unlike v1 (superseded by ADR-015), this engine does NOT
// unconditionally clear the KV cache on every call: kvTokens tracks exactly
// what is currently decoded for sequence 0, and completion() reuses the
// longest common prefix between kvTokens and the new prompt (B1/S21),
// trading v1's simplicity for the turn time this trades away (see ADR-015
// for the ~6.4min -> ~60-90s consequence).
type cgoEngine struct {
	mu sync.Mutex

	model *C.struct_llama_model
	lctx  *C.struct_llama_context
	vocab *C.struct_llama_vocab

	// loaded is the loadKey the currently-loaded model/context was created
	// with; ensureLoaded reloads only when this changes (AC-016-4).
	loaded    loadKey
	loadCount int

	// kvTokens mirrors exactly what is currently decoded in the KV cache
	// for sequence 0: the retained prefix of the previous prompt, plus
	// every token that has since gone through a successful llama_decode
	// (S21 invariant). Reset to nil (with a full llama_memory_clear) on
	// any error or abort, so the next call always starts from a
	// known-consistent KV cache rather than a partially-decoded one.
	kvTokens []C.llama_token

	// parked, activeCoreHash/activeCoreValid, and parkGen implement B2
	// (window-core KV parking, ADR-015 point 8). activeCoreValid reports
	// whether the sequence-0 prefix currently described by kvTokens starts
	// with a known core, and activeCoreHash is that core's hash; both are
	// meaningless when parking is off (opts.CoreCacheParking == false), in
	// which case completion() never touches them.
	parked          [coreCacheSlots]parkedCore
	activeCoreHash  [sha256.Size]byte
	activeCoreValid bool
	parkGen         uint64

	keepAliveTimer *time.Timer
}

func newEngine() engine {
	return &cgoEngine{}
}

// ensureLoaded lazily loads the model on first use, or reloads it if the
// load-affecting subset of opts (loadKey) differs from what's currently
// loaded (e.g. after an idle keep-alive unload, or a genuinely different
// model/n_ctx/n_threads/n_batch/kv_cache_type). Caller must hold e.mu.
func (e *cgoEngine) ensureLoaded(opts Options) error {
	key := opts.loadKey()
	if e.lctx != nil && e.loaded == key {
		return nil
	}
	e.unloadLocked()

	// ADR-017/C3: memguard's preLoad hook gets a chance to refuse the load
	// (AC-018-2, e.g. insufficient MemAvailable) before any C allocation
	// happens. A no-op (nil error) until SetMemoryHooks is called --
	// memguard.enabled=false (the default) or a platform with no PSI
	// (AC-018-6) behave exactly as before C3.
	estimate := estimateMemory(opts)
	if err := callPreLoadHook(estimate); err != nil {
		return err
	}

	backendInitOnce.Do(func() {
		C.llama_backend_init()
	})

	cPath := C.CString(opts.ModelPath)
	defer C.free(unsafe.Pointer(cPath))

	mparams := C.llama_model_default_params()
	mparams.n_gpu_layers = 0 // CPU-only: this is the whole point on a 1GB-RAM ARM64 box

	model := C.llama_model_load_from_file(cPath, mparams)
	if model == nil {
		return fmt.Errorf("localllm: failed to load model %q", opts.ModelPath)
	}

	vocab := C.llama_model_get_vocab(model)

	cparams := C.llama_context_default_params()
	cparams.n_ctx = C.uint32_t(opts.NCtx)
	if opts.CoreCacheParking {
		// B2/ADR-015 point 8: kv_unified puts every sequence in one
		// n_ctx-sized buffer instead of dividing it by n_seq_max, so parking
		// coreCacheSlots extra cores does not cost extra KV bytes -- only
		// the cells their tokens actually occupy, already covered by the
		// existing n_ctx budget (see the architecture plan's measured
		// rationale for why n_ctx did not need to grow for this).
		cparams.n_seq_max = C.uint32_t(1 + coreCacheSlots)
		cparams.kv_unified = true
	}
	// n_batch is the *logical* cap llama_decode enforces per call
	// (GGML_ASSERT(n_tokens_all <= cparams.n_batch)) -- it must cover the
	// whole prompt in one shot, since completion() submits it as a single
	// llama_batch_get_one (no manual chunking loop). Bounding it to NBatch
	// (256 by default) instead of n_ctx crashed the process with SIGABRT on
	// any prompt over ~256 tokens -- trivially true once the real agent
	// system prompt + tool schemas are included, not just bare test
	// prompts. n_ubatch is the *physical* compute chunk size and is what
	// actually drives the compute-buffer memory reservation (S18/S29); it
	// stays at opts.NBatch so this fix does not change the measured RAM
	// budget, only what llama_decode will accept without asserting.
	cparams.n_batch = C.uint32_t(opts.NCtx)
	cparams.n_ubatch = C.uint32_t(opts.NBatch)
	cparams.n_threads = C.int32_t(opts.NThreads)
	cparams.n_threads_batch = C.int32_t(opts.NThreads)
	if opts.KVCacheType == "f16" {
		cparams.type_k = C.GGML_TYPE_F16
		cparams.type_v = C.GGML_TYPE_F16
	} else {
		cparams.type_k = C.GGML_TYPE_Q8_0
		cparams.type_v = C.GGML_TYPE_Q8_0
	}

	lctx := C.llama_init_from_model(model, cparams)
	if lctx == nil {
		C.llama_model_free(model)
		return fmt.Errorf("localllm: failed to create llama context (n_ctx=%d)", opts.NCtx)
	}

	// ADR-015 point 5: wire cancellation into llama_decode itself (not
	// just the Go-level ctx.Done() check between decode calls in
	// completion()), so cancelling mid-prefill on a long prompt actually
	// stops instead of running to completion. See the kuromatsu_abort_*
	// helpers in the cgo preamble above.
	C.llama_set_abort_callback(lctx, C.ggml_abort_callback(C.kuromatsu_abort_cb), nil)

	e.model = model
	e.lctx = lctx
	e.vocab = vocab
	e.loaded = key
	e.loadCount++

	// ADR-017/C3: memguard's postLoad hook (also a no-op by default) now
	// gets to apply debug.SetMemoryLimit/SetGCPercent with the same
	// estimate PreLoad already vetted (AC-018-1).
	callPostLoadHook(estimate)
	return nil
}

// unloadLocked frees the model+context and resets everything that is only
// meaningful while they're loaded (including kvTokens: once the context is
// gone, the KV cache it described no longer exists). Caller must hold e.mu.
func (e *cgoEngine) unloadLocked() {
	if e.lctx != nil {
		C.llama_free(e.lctx)
		e.lctx = nil
	}
	if e.model != nil {
		C.llama_model_free(e.model)
		e.model = nil
	}
	e.vocab = nil
	e.loaded = loadKey{}
	e.kvTokens = nil
	// The context (and every sequence in it, parked cores included) is
	// gone -- B2 state describing it would otherwise dangle.
	e.parked = [coreCacheSlots]parkedCore{}
	e.activeCoreValid = false
}

func (e *cgoEngine) unload() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keepAliveTimer != nil {
		e.keepAliveTimer.Stop()
	}
	e.unloadLocked()
}

// clearSeq0 resets sequence 0 (the active conversation) to empty. With
// parking off this is byte-identical to pre-B2 behavior (a full
// llama_memory_clear). With parking on, a full clear would also wipe
// whatever cores are parked in sequences 1..coreCacheSlots (confirmed
// against llama.cpp's real clear() semantics: it resets every stream in
// unified mode), silently defeating B2 on the very first cold miss or
// abort after switching it on -- so parking uses llama_memory_seq_rm
// against sequence 0 only instead, which the header documents as never
// failing for a whole-sequence removal.
func (e *cgoEngine) clearSeq0(mem C.llama_memory_t, parking bool) {
	if parking {
		C.llama_memory_seq_rm(mem, C.llama_seq_id(0), C.llama_pos(0), C.llama_pos(-1))
		return
	}
	C.llama_memory_clear(mem, true)
}

// resolveCoreTokens tokenizes prompt[:coreEnd] on its own, the same way
// completion tokenizes the full prompt. Returns ok=false if there is
// nothing to park (coreEnd<=0) or tokenization fails.
func (e *cgoEngine) resolveCoreTokens(prompt string, coreEnd int) (tokens []C.llama_token, ok bool) {
	if coreEnd <= 0 || coreEnd > len(prompt) {
		return nil, false
	}
	cCore := C.CString(prompt[:coreEnd])
	defer C.free(unsafe.Pointer(cCore))
	coreLen := C.int32_t(coreEnd)

	n := int(-C.llama_tokenize(e.vocab, cCore, coreLen, nil, 0, true, true))
	if n <= 0 {
		return nil, false
	}
	buf := make([]C.llama_token, n)
	got := int(C.llama_tokenize(e.vocab, cCore, coreLen, &buf[0], C.int32_t(n), true, true))
	if got < 0 {
		return nil, false
	}
	return buf[:got], true
}

// findParkedCore returns the parked slot matching hash, if any, bumping its
// generation counter (it was just used). nil if the core is not parked.
func (e *cgoEngine) findParkedCore(hash [sha256.Size]byte) *parkedCore {
	for i := range e.parked {
		if e.parked[i].valid && e.parked[i].hash == hash {
			e.parkGen++
			e.parked[i].gen = e.parkGen
			return &e.parked[i]
		}
	}
	return nil
}

// parkCore records tokens (already resident in sequence 0, positions
// [0,nCore)) as a reusable core under hash, copying its KV cells into a
// free or least-recently-used slot's sequence. Idempotent: re-parking a
// hash already resident just touches its generation counter.
func (e *cgoEngine) parkCore(mem C.llama_memory_t, hash [sha256.Size]byte, nCore int, tokens []C.llama_token) {
	if existing := e.findParkedCore(hash); existing != nil {
		return
	}

	valid := make([]bool, len(e.parked))
	gens := make([]uint64, len(e.parked))
	for i := range e.parked {
		valid[i] = e.parked[i].valid
		gens[i] = e.parked[i].gen
	}
	slot := pickCoreCacheSlot(valid, gens)

	if e.parked[slot].valid {
		// Evicting an occupied slot: release its cells first so they are
		// not left tagged with a sequence id nothing points at any more.
		C.llama_memory_seq_rm(mem, e.parked[slot].seqID, C.llama_pos(0), C.llama_pos(-1))
	}

	seqID := C.llama_seq_id(slot + 1) // seq 0 is the active conversation
	C.llama_memory_seq_cp(mem, C.llama_seq_id(0), seqID, C.llama_pos(0), C.llama_pos(nCore))

	e.parkGen++
	e.parked[slot] = parkedCore{
		valid:  true,
		seqID:  seqID,
		hash:   hash,
		nCore:  nCore,
		tokens: append([]C.llama_token(nil), tokens...),
		gen:    e.parkGen,
	}
}

func (e *cgoEngine) completion(ctx context.Context, prompt string, coreEnd int, opts Options) (CompletionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Keep-alive is armed from the END of this call (deferred below), not
	// the start (ADR-015 point 4): the idle-unload countdown should
	// measure time since the model was actually last used, not overlap
	// with this call's own execution. Any pending timer from a previous
	// call is stopped up front so a short KeepAliveSecs can't fire and
	// unload the model out from under this call while it's running.
	// KeepAliveSecs < 0 means never unload (AC-016-9).
	if e.keepAliveTimer != nil {
		e.keepAliveTimer.Stop()
		e.keepAliveTimer = nil
	}
	defer func() {
		if opts.KeepAliveSecs >= 0 {
			e.keepAliveTimer = time.AfterFunc(time.Duration(opts.KeepAliveSecs)*time.Second, func() {
				e.mu.Lock()
				defer e.mu.Unlock()
				e.unloadLocked()
			})
		}
	}()

	if err := e.ensureLoaded(opts); err != nil {
		return CompletionResult{}, err
	}

	// Arm the abort flag for the duration of this call only. stopAbort
	// unregisters the AfterFunc so a ctx that outlives this call (or is
	// already done, e.g. reused by a caller) can't flip the flag against a
	// *later*, unrelated completion() call.
	C.kuromatsu_set_abort(0)
	stopAbort := context.AfterFunc(ctx, func() { C.kuromatsu_set_abort(1) })
	defer stopAbort()

	callStart := time.Now()

	cPrompt := C.CString(prompt)
	defer C.free(unsafe.Pointer(cPrompt))
	promptLen := C.int32_t(len(prompt))

	nPromptTokens := int(-C.llama_tokenize(e.vocab, cPrompt, promptLen, nil, 0, true, true))
	if nPromptTokens+contextMargin > opts.NCtx {
		logger.WarnCF("localllm", "prompt exceeds context window", map[string]any{
			"prompt_tokens": nPromptTokens,
			"n_ctx":         opts.NCtx,
			"margin":        contextMargin,
		})
		return CompletionResult{}, ErrContextOverflow
	}

	tokens := make([]C.llama_token, nPromptTokens)
	n := int(C.llama_tokenize(e.vocab, cPrompt, promptLen, &tokens[0], C.int32_t(nPromptTokens), true, true))
	if n < 0 {
		return CompletionResult{}, fmt.Errorf("localllm: failed to tokenize prompt")
	}
	tokens = tokens[:n]

	mem := C.llama_get_memory(e.lctx)

	// B2/ADR-015 point 8: if this call's window has a "core" (coreEnd>0)
	// and parking is on, make sure sequence 0 holds the *right* core
	// before B1's own prefix match runs below -- either it already does
	// (same window as last call: nothing to do here, B1 finds the shared
	// prefix on its own), or a parked copy exists and gets restored in
	// milliseconds via seq_cp, or neither and B1 below decodes it cold
	// (parked afterwards, once the decode actually succeeds -- see the
	// generation loop).
	var nCore int
	var coreHash [sha256.Size]byte
	if opts.CoreCacheParking {
		if candidate, ok := e.resolveCoreTokens(prompt, coreEnd); ok {
			// Guard against a tokenizer merge spanning the core/rest
			// boundary: only trust nCore if tokenizing the core alone
			// reproduces an exact prefix of the full tokenization.
			if n := commonPrefixLen(candidate, tokens); n == len(candidate) {
				nCore = n
				coreHash = sha256.Sum256([]byte(prompt[:coreEnd]))
			}
		}
		if nCore > 0 && !(e.activeCoreValid && e.activeCoreHash == coreHash) {
			if slot := e.findParkedCore(coreHash); slot != nil {
				C.llama_memory_seq_rm(mem, C.llama_seq_id(0), C.llama_pos(0), C.llama_pos(-1))
				C.llama_memory_seq_cp(mem, slot.seqID, C.llama_seq_id(0), C.llama_pos(0), C.llama_pos(slot.nCore))
				if int(C.llama_memory_seq_pos_max(mem, C.llama_seq_id(0)))+1 == slot.nCore {
					e.kvTokens = append([]C.llama_token(nil), slot.tokens...)
					e.activeCoreHash = coreHash
					e.activeCoreValid = true
				} else {
					// seq_cp is void -- this is the only way to notice it
					// didn't land as expected. Don't trust a half-restored
					// sequence 0; treat it as empty and let B1 below decode
					// everything cold.
					C.llama_memory_seq_rm(mem, C.llama_seq_id(0), C.llama_pos(0), C.llama_pos(-1))
					e.kvTokens = nil
					e.activeCoreValid = false
				}
			} else {
				e.activeCoreValid = false
			}
		}
	}

	// B1/ADR-015/S21: reuse the longest common prefix already resident in
	// the KV cache instead of unconditionally clearing it (v1's
	// behavior). nCommon == len(tokens) (an exact repeat of the last
	// prompt) is pulled back by one token so at least one token is
	// freshly decoded -- with zero fresh tokens there would be no logits
	// to sample the next token from (AC-016-2).
	nCommon := commonPrefixLen(e.kvTokens, tokens)
	if nCommon == len(tokens) {
		nCommon--
	}
	if nCommon > 0 {
		if !C.llama_memory_seq_rm(mem, C.llama_seq_id(0), C.llama_pos(nCommon), C.llama_pos(-1)) {
			// Partial removal refused by llama.cpp -- fall back to a full
			// miss rather than risk kvTokens describing a KV cache state
			// that no longer matches reality (S21 invariant: never a
			// partially-inconsistent state, only a slower full miss).
			e.clearSeq0(mem, opts.CoreCacheParking)
			nCommon = 0
			e.activeCoreValid = false
		}
	} else {
		e.clearSeq0(mem, opts.CoreCacheParking)
		e.activeCoreValid = false
	}
	cachedTokens := nCommon
	// Explicit copy: tokens[:nCommon] shares tokens' backing array, and
	// kvTokens must remain valid/unaliased after this function returns.
	e.kvTokens = append([]C.llama_token(nil), tokens[:nCommon]...)

	sparams := C.llama_sampler_chain_default_params()
	smpl := C.llama_sampler_chain_init(sparams)
	defer C.llama_sampler_free(smpl)
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_top_k(C.int32_t(opts.TopK)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_top_p(C.float(opts.TopP), C.size_t(1)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_temp(C.float(opts.Temperature)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_dist(C.uint32_t(time.Now().UnixNano())))

	pending := tokens[nCommon:] // the delta this call actually needs to decode
	batch := C.llama_batch_get_one(&pending[0], C.int32_t(len(pending)))
	// pendingTokens is exactly what `batch` will decode next; only
	// appended to kvTokens once that decode has actually succeeded (S21
	// invariant -- a token that's never decoded, e.g. because MaxPredict
	// or ctx cancellation cuts the loop short, must not be recorded as
	// cached).
	pendingTokens := pending

	var out strings.Builder
	buf := make([]byte, 256)
	outputTokens := 0
	stoppedByEOG := false
	var prefill time.Duration
	firstDecode := true
	var stepBuf [1]C.llama_token

	// clearOnFailure restores sequence 0 to a known-empty state and drops
	// kvTokens, per the AC-016-3 invariant: any error or abort during
	// decode must never leave kvTokens describing a partially-decoded KV
	// cache. Seq-scoped (clearSeq0), not a full clear, when B2 parking is
	// on: an abort/failure on the active conversation must not also wipe
	// whatever cores are parked in other sequences.
	clearOnFailure := func() {
		e.clearSeq0(mem, opts.CoreCacheParking)
		e.kvTokens = nil
		e.activeCoreValid = false
	}

	for outputTokens < opts.MaxPredict {
		select {
		case <-ctx.Done():
			clearOnFailure()
			return CompletionResult{
				Text:         out.String(),
				PromptTokens: nPromptTokens,
				OutputTokens: outputTokens,
				CachedTokens: cachedTokens,
			}, ctx.Err()
		default:
		}

		nCtxUsed := int(C.llama_memory_seq_pos_max(mem, C.llama_seq_id(0))) + 1
		if opts.CoreCacheParking {
			// Sequence 0's own cells are already counted above. Add cells
			// occupied by any OTHER parked core (kv_unified puts every
			// sequence in the same n_ctx-sized buffer, so those cells
			// really do compete for the same budget) -- but not the
			// currently-active one, whose cells are the same physical
			// cells seq 0 just counted (would double-count otherwise).
			for i := range e.parked {
				if e.parked[i].valid && !(e.activeCoreValid && e.parked[i].hash == e.activeCoreHash) {
					nCtxUsed += e.parked[i].nCore
				}
			}
		}
		if nCtxUsed+int(batch.n_tokens) > opts.NCtx {
			logger.WarnCF("localllm", "context window filled mid-generation", map[string]any{
				"n_ctx_used":           nCtxUsed,
				"n_ctx":                opts.NCtx,
				"output_tokens_so_far": outputTokens,
			})
			clearOnFailure()
			return CompletionResult{}, ErrContextOverflow
		}

		decodeStart := time.Now()
		ret := C.llama_decode(e.lctx, batch)
		decodeElapsed := time.Since(decodeStart)
		isPrefillDecode := firstDecode
		if firstDecode {
			prefill = decodeElapsed
			firstDecode = false
		}
		if ret != 0 {
			clearOnFailure()
			// ret == 2: aborted mid-decode (llama.h). If our own
			// abort_callback fired because ctx was cancelled/timed out,
			// surface that as ctx.Err() (AC-016-5) rather than a generic
			// decode-failed error.
			if ret == 2 {
				if cErr := ctx.Err(); cErr != nil {
					return CompletionResult{
						Text:         out.String(),
						PromptTokens: nPromptTokens,
						OutputTokens: outputTokens,
						CachedTokens: cachedTokens,
					}, cErr
				}
			}
			return CompletionResult{}, fmt.Errorf("localllm: decode failed (ret=%d)", int(ret))
		}
		e.kvTokens = append(e.kvTokens, pendingTokens...)

		// The prefill decode above (whether it was a cold miss or covered
		// only a partial-history delta) always includes the whole of any
		// new core -- pending is tokens[nCommon:], and nCommon<nCore never
		// happens once activeCoreValid is true for this hash (see the pre-
		// step). Park it now, right as it's confirmed resident, rather
		// than waiting for the whole call to succeed: a later abort/error
		// during generation must not cost this call's own prefill work.
		if isPrefillDecode && opts.CoreCacheParking && nCore > 0 && !(e.activeCoreValid && e.activeCoreHash == coreHash) {
			e.parkCore(mem, coreHash, nCore, e.kvTokens[:nCore])
			e.activeCoreHash = coreHash
			e.activeCoreValid = true
		}

		newToken := C.llama_sampler_sample(smpl, e.lctx, -1)

		if C.llama_vocab_is_eog(e.vocab, newToken) {
			stoppedByEOG = true
			break
		}

		nChars := C.llama_token_to_piece(e.vocab, newToken, (*C.char)(unsafe.Pointer(&buf[0])), C.int32_t(len(buf)), 0, true)
		if nChars < 0 {
			break
		}
		out.Write(buf[:nChars])
		outputTokens++

		stepBuf[0] = newToken
		batch = C.llama_batch_get_one(&stepBuf[0], 1)
		pendingTokens = stepBuf[:]
	}

	generation := time.Since(callStart) - prefill

	genTokS := 0.0
	if generation > 0 {
		genTokS = float64(outputTokens) / generation.Seconds()
	}
	logger.InfoCF("localllm", "completion", map[string]any{
		"prompt_tokens": nPromptTokens,
		"cached_tokens": cachedTokens,
		"new_tokens":    nPromptTokens - cachedTokens,
		"output_tokens": outputTokens,
		"prefill_ms":    prefill.Milliseconds(),
		"gen_ms":        generation.Milliseconds(),
		"gen_tok_s":     genTokS,
		"n_threads":     opts.NThreads,
	})

	return CompletionResult{
		Text:         out.String(),
		PromptTokens: nPromptTokens,
		OutputTokens: outputTokens,
		StoppedByEOG: stoppedByEOG,
		CachedTokens: cachedTokens,
		Prefill:      prefill,
		Generation:   generation,
	}, nil
}
