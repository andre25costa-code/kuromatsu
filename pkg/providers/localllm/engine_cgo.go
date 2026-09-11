//go:build nativellm && cgo

package localllm

/*
#cgo CFLAGS: -I${SRCDIR}/../../../llama.cpp/include -I${SRCDIR}/../../../llama.cpp/ggml/include
#cgo LDFLAGS: -L${SRCDIR}/../../../llama.cpp/build-native/lib
#cgo LDFLAGS: -lllama -lggml -lggml-cpu -lggml-base -lstdc++ -lm -lpthread
#include <stdlib.h>
#include "llama.h"
*/
import "C"

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// built is true only on a binary compiled with -tags nativellm and
// CGO_ENABLED=1 (see Makefile's build-native target). The stub build
// (engine_stub.go) defines the complementary false value.
const built = true

// contextMargin is reserved headroom (in tokens) so a prompt that exactly
// fills n_ctx still gets flagged as overflow before generation, rather
// than succeeding with zero room to produce output.
const contextMargin = 64

var backendInitOnce sync.Once

// cgoEngine binds one loaded GGUF model + llama_context. ADR-003: a single
// mutex serializes every call, matching the "one context per process" 1GB-
// RAM design; there is no cross-call KV-cache reuse (v1 always clears
// memory and reprocesses the full prompt), which trades a slower first
// token on every turn for a much simpler implementation -- acceptable for
// the "no immediate response needed" workload this targets (S01).
type cgoEngine struct {
	mu sync.Mutex

	model *C.struct_llama_model
	lctx  *C.struct_llama_context
	vocab *C.struct_llama_vocab

	loadedPath string
	loadedOpts Options

	keepAliveTimer *time.Timer
}

func newEngine() engine {
	return &cgoEngine{}
}

// ensureLoaded lazily loads the model on first use, or reloads it if the
// requested path/options differ from what's currently loaded (e.g. after
// an idle keep-alive unload). Caller must hold e.mu.
func (e *cgoEngine) ensureLoaded(opts Options) error {
	if e.lctx != nil && e.loadedPath == opts.ModelPath && e.loadedOpts == opts {
		return nil
	}
	e.unloadLocked()

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
	cparams.n_batch = C.uint32_t(opts.NBatch)
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

	e.model = model
	e.lctx = lctx
	e.vocab = vocab
	e.loadedPath = opts.ModelPath
	e.loadedOpts = opts
	return nil
}

// unloadLocked frees the model+context. Caller must hold e.mu.
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
	e.loadedPath = ""
}

func (e *cgoEngine) unload() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keepAliveTimer != nil {
		e.keepAliveTimer.Stop()
	}
	e.unloadLocked()
}

func (e *cgoEngine) completion(ctx context.Context, prompt string, opts Options) (CompletionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.keepAliveTimer != nil {
		e.keepAliveTimer.Stop()
	}
	if opts.KeepAliveSecs > 0 {
		e.keepAliveTimer = time.AfterFunc(time.Duration(opts.KeepAliveSecs)*time.Second, func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.unloadLocked()
		})
	}

	if err := e.ensureLoaded(opts); err != nil {
		return CompletionResult{}, err
	}

	// v1 has no cross-call KV reuse: every completion starts a fresh
	// sequence, so tokenization always adds BOS (matches examples/
	// simple-chat's is_first == true path).
	C.llama_memory_clear(C.llama_get_memory(e.lctx), true)

	cPrompt := C.CString(prompt)
	defer C.free(unsafe.Pointer(cPrompt))
	promptLen := C.int32_t(len(prompt))

	nPromptTokens := int(-C.llama_tokenize(e.vocab, cPrompt, promptLen, nil, 0, true, true))
	if nPromptTokens+contextMargin > opts.NCtx {
		return CompletionResult{}, ErrContextOverflow
	}

	tokens := make([]C.llama_token, nPromptTokens)
	n := int(C.llama_tokenize(e.vocab, cPrompt, promptLen, &tokens[0], C.int32_t(nPromptTokens), true, true))
	if n < 0 {
		return CompletionResult{}, fmt.Errorf("localllm: failed to tokenize prompt")
	}
	tokens = tokens[:n]

	sparams := C.llama_sampler_chain_default_params()
	smpl := C.llama_sampler_chain_init(sparams)
	defer C.llama_sampler_free(smpl)
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_top_k(C.int32_t(opts.TopK)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_top_p(C.float(opts.TopP), C.size_t(1)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_temp(C.float(opts.Temperature)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_dist(C.uint32_t(time.Now().UnixNano())))

	batch := C.llama_batch_get_one(&tokens[0], C.int32_t(len(tokens)))

	var out strings.Builder
	buf := make([]byte, 256)
	outputTokens := 0
	stoppedByEOG := false

	for outputTokens < opts.MaxPredict {
		select {
		case <-ctx.Done():
			return CompletionResult{Text: out.String(), PromptTokens: nPromptTokens, OutputTokens: outputTokens}, ctx.Err()
		default:
		}

		nCtxUsed := int(C.llama_memory_seq_pos_max(C.llama_get_memory(e.lctx), 0)) + 1
		if nCtxUsed+int(batch.n_tokens) > opts.NCtx {
			return CompletionResult{}, ErrContextOverflow
		}

		if ret := C.llama_decode(e.lctx, batch); ret != 0 {
			return CompletionResult{}, fmt.Errorf("localllm: decode failed (ret=%d)", int(ret))
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

		tokens = tokens[:1]
		tokens[0] = newToken
		batch = C.llama_batch_get_one(&tokens[0], 1)
	}

	return CompletionResult{
		Text:         out.String(),
		PromptTokens: nPromptTokens,
		OutputTokens: outputTokens,
		StoppedByEOG: stoppedByEOG,
	}, nil
}
