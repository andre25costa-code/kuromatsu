package config

import (
	"os"
	"path/filepath"
	"testing"
)

func withNativeBuilt(t *testing.T, built bool) {
	t.Helper()
	old := nativeBuilt
	nativeBuilt = func() bool { return built }
	t.Cleanup(func() { nativeBuilt = old })
}

// withHome points GetHome() at a fresh temp dir for the duration of the
// test, so ResolveModelPath's "<home>/models/<id>.gguf" lookup is
// deterministic regardless of the machine running the test.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	return home
}

func writeTestGGUF(t *testing.T, home, filename string) {
	t.Helper()
	dir := filepath.Join(home, "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func nativeTestModelList() []*ModelConfig {
	return []*ModelConfig{
		{ModelName: nativeModelName, Provider: "native", Model: "bonsai-test"},
	}
}

func TestApplyNativeFallback_NilConfig(t *testing.T) {
	withNativeBuilt(t, true)
	ApplyNativeFallback(nil) // must not panic
}

func TestApplyNativeFallback_NoopWhenNotBuilt(t *testing.T) {
	withNativeBuilt(t, false)
	home := withHome(t)
	writeTestGGUF(t, home, "bonsai-test.gguf")

	cfg := &Config{ModelList: nativeTestModelList()}
	ApplyNativeFallback(cfg)

	if cfg.ModelList[0].Enabled {
		t.Fatal("entry should stay disabled on a build without the nativellm engine (BR-002)")
	}
	if cfg.Agents.Defaults.ModelName != "" {
		t.Fatalf("ModelName = %q, want empty", cfg.Agents.Defaults.ModelName)
	}
}

func TestApplyNativeFallback_NoopWhenSeededEntryRemoved(t *testing.T) {
	withNativeBuilt(t, true)
	cfg := &Config{ModelList: []*ModelConfig{{ModelName: "something-else", Model: "x"}}}

	ApplyNativeFallback(cfg)

	if cfg.Agents.Defaults.ModelName != "" {
		t.Fatalf("ModelName = %q, want empty (user removed the seeded entry)", cfg.Agents.Defaults.ModelName)
	}
}

func TestApplyNativeFallback_NoopWhenGGUFMissing(t *testing.T) {
	withNativeBuilt(t, true)
	withHome(t) // no GGUF written under <home>/models

	cfg := &Config{ModelList: nativeTestModelList()}
	ApplyNativeFallback(cfg)

	if cfg.ModelList[0].Enabled {
		t.Fatal("entry should stay disabled when the GGUF is absent (BR-002)")
	}
	if cfg.Agents.Defaults.ModelName != "" {
		t.Fatalf("ModelName = %q, want empty", cfg.Agents.Defaults.ModelName)
	}
}

// AC-003-1: no API key anywhere and no explicit default -> native becomes
// the effective default model.
func TestApplyNativeFallback_BecomesDefaultWhenNoAPIKeys(t *testing.T) {
	withNativeBuilt(t, true)
	home := withHome(t)
	writeTestGGUF(t, home, "bonsai-test.gguf")

	cfg := &Config{ModelList: nativeTestModelList()}
	ApplyNativeFallback(cfg)

	if !cfg.ModelList[0].Enabled {
		t.Fatal("entry should be enabled once the GGUF is present and the binary is built")
	}
	if cfg.Agents.Defaults.ModelName != nativeModelName {
		t.Fatalf("ModelName = %q, want %q", cfg.Agents.Defaults.ModelName, nativeModelName)
	}
	if len(cfg.Agents.Defaults.ModelFallbacks) != 0 {
		t.Fatalf("ModelFallbacks = %v, want empty (native is the primary, not a fallback)", cfg.Agents.Defaults.ModelFallbacks)
	}
}

// AC-003-2: a configured API key means the chain resolves the cloud model
// first, with native appended as the last fallback instead of taking over
// as the default.
func TestApplyNativeFallback_AppendedAsFallbackWhenAnAPIKeyExists(t *testing.T) {
	withNativeBuilt(t, true)
	home := withHome(t)
	writeTestGGUF(t, home, "bonsai-test.gguf")

	cloud := &ModelConfig{ModelName: "gpt", Provider: "openai", Model: "gpt-5.4"}
	cloud.SetAPIKey("sk-test")

	cfg := &Config{ModelList: append([]*ModelConfig{cloud}, nativeTestModelList()...)}
	ApplyNativeFallback(cfg)

	if cfg.Agents.Defaults.ModelName != "" {
		t.Fatalf("ModelName = %q, want empty (native must not take over the default when a key exists)", cfg.Agents.Defaults.ModelName)
	}
	if len(cfg.Agents.Defaults.ModelFallbacks) != 1 || cfg.Agents.Defaults.ModelFallbacks[0] != nativeModelName {
		t.Fatalf("ModelFallbacks = %v, want [%q]", cfg.Agents.Defaults.ModelFallbacks, nativeModelName)
	}
}

func TestApplyNativeFallback_RespectsExplicitDefault(t *testing.T) {
	withNativeBuilt(t, true)
	home := withHome(t)
	writeTestGGUF(t, home, "bonsai-test.gguf")

	cfg := &Config{ModelList: nativeTestModelList()}
	cfg.Agents.Defaults.ModelName = "some-other-model"
	ApplyNativeFallback(cfg)

	if cfg.Agents.Defaults.ModelName != "some-other-model" {
		t.Fatalf("ModelName = %q, want unchanged", cfg.Agents.Defaults.ModelName)
	}
	if len(cfg.Agents.Defaults.ModelFallbacks) != 1 || cfg.Agents.Defaults.ModelFallbacks[0] != nativeModelName {
		t.Fatalf("ModelFallbacks = %v, want [%q]", cfg.Agents.Defaults.ModelFallbacks, nativeModelName)
	}
}

// TestNativeLimits_ReadsExtraBodyKeys covers A8/FR-015 AC-015-6: NativeLimits
// must read exactly the ExtraBody keys nativeOptionsFromModelConfig reads,
// so ContextWindow/MaxTokens can't silently drift from the engine's real
// n_ctx/max_predict.
func TestNativeLimits_ReadsExtraBodyKeys(t *testing.T) {
	nCtx, maxPredict, ok := NativeLimits(&ModelConfig{
		ExtraBody: map[string]any{"n_ctx": 2048, "max_predict": 512, "kv_cache_type": "q8_0"},
	})
	if !ok {
		t.Fatal("NativeLimits() ok = false, want true")
	}
	if nCtx != 2048 || maxPredict != 512 {
		t.Fatalf("NativeLimits() = (%d, %d), want (2048, 512)", nCtx, maxPredict)
	}
}

func TestNativeLimits_NilOrEmptyExtraBodyIsNotOK(t *testing.T) {
	if _, _, ok := NativeLimits(nil); ok {
		t.Fatal("NativeLimits(nil) ok = true, want false")
	}
	if _, _, ok := NativeLimits(&ModelConfig{}); ok {
		t.Fatal("NativeLimits(no ExtraBody) ok = true, want false")
	}
	if _, _, ok := NativeLimits(&ModelConfig{ExtraBody: map[string]any{"kv_cache_type": "q8_0"}}); ok {
		t.Fatal("NativeLimits(unrelated ExtraBody keys) ok = true, want false")
	}
}

func TestNativeLimits_PartialKeysStillOK(t *testing.T) {
	nCtx, maxPredict, ok := NativeLimits(&ModelConfig{ExtraBody: map[string]any{"n_ctx": 4096}})
	if !ok || nCtx != 4096 || maxPredict != 0 {
		t.Fatalf("NativeLimits(n_ctx only) = (%d, %d, %v), want (4096, 0, true)", nCtx, maxPredict, ok)
	}
}

func TestApplyNativeFallback_IdempotentOnRepeatedCalls(t *testing.T) {
	withNativeBuilt(t, true)
	home := withHome(t)
	writeTestGGUF(t, home, "bonsai-test.gguf")

	cloud := &ModelConfig{ModelName: "gpt", Provider: "openai", Model: "gpt-5.4"}
	cloud.SetAPIKey("sk-test")

	cfg := &Config{ModelList: append([]*ModelConfig{cloud}, nativeTestModelList()...)}
	ApplyNativeFallback(cfg)
	ApplyNativeFallback(cfg)

	if len(cfg.Agents.Defaults.ModelFallbacks) != 1 {
		t.Fatalf("ModelFallbacks = %v, want exactly one entry after two calls", cfg.Agents.Defaults.ModelFallbacks)
	}
}
