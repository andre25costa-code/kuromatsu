//go:build !nativellm || !cgo

package localllm

import "context"

// built is false on the default (CGO_ENABLED=0, no nativellm tag) build.
// See Built() in types.go and its complementary definition in
// engine_cgo.go (E5). NFR-003, BR-004.
const built = false

// stubEngine is used whenever the binary was not compiled with the
// nativellm cgo engine (engine_cgo.go, E5). Every method returns
// ErrNotBuilt so the rest of the package (and the provider factory in E6)
// can be developed and tested without a C toolchain. AC-001-2.
type stubEngine struct{}

func newEngine() engine {
	return stubEngine{}
}

func (stubEngine) completion(ctx context.Context, prompt string, opts Options) (CompletionResult, error) {
	return CompletionResult{}, ErrNotBuilt
}

func (stubEngine) unload() {}
