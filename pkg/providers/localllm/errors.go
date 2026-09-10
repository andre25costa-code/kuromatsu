package localllm

import "errors"

// ErrNotBuilt is returned by every engine method on a binary compiled
// without the nativellm build tag (see engine_stub.go). FR-001/AC-001-2.
var ErrNotBuilt = errors.New(
	"localllm: this binary was built without native inference support " +
		"(rebuild with -tags nativellm and CGO_ENABLED=1, e.g. `make build-native`)")

// ErrModelNotFound is returned by ResolveModelPath when no GGUF file is
// found for the given model id in any of the search locations.
var ErrModelNotFound = errors.New("localllm: model file not found")

// ErrContextOverflow signals that the rendered prompt exceeds the engine's
// n_ctx. The message text deliberately matches providers.contextOverflowPatterns
// (see pkg/providers/error_classifier.go) so the fallback chain classifies
// it as FailoverContextOverflow (non-retriable, triggers summarization)
// without localllm importing pkg/providers and creating an import cycle.
// AC-001-3.
var ErrContextOverflow = errors.New("localllm: prompt exceeds context window (context_length_exceeded)")
