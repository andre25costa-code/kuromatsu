package localllm

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestOptions_WithDefaults_NThreadsBoundedByCPUAndFour covers ADR-015 point
// 6: n_threads defaults to min(runtime.NumCPU(), 4), never more than the
// hardware actually offers (the demetrius deploy target has 1 physical
// core / 2 HT threads, where a flat default of 4 would oversubscribe).
func TestOptions_WithDefaults_NThreadsBoundedByCPUAndFour(t *testing.T) {
	o := Options{}.WithDefaults()

	want := runtime.NumCPU()
	if want > 4 {
		want = 4
	}
	if o.NThreads != want {
		t.Fatalf("NThreads = %d, want min(runtime.NumCPU(), 4) = %d", o.NThreads, want)
	}
	if o.NThreads < 1 {
		t.Fatalf("NThreads = %d, want >= 1", o.NThreads)
	}
}

func TestOptions_WithDefaults_ExplicitNThreadsNotClamped(t *testing.T) {
	// An explicit, caller-supplied NThreads (e.g. from ExtraBody.n_threads)
	// must be left alone even if it exceeds min(NumCPU,4) -- the clamp is
	// only a *default*, not a hard cap (ADR-015 point 6 says "configurable").
	o := Options{NThreads: 7}.WithDefaults()
	if o.NThreads != 7 {
		t.Fatalf("NThreads = %d, want unchanged 7 (explicit value must not be clamped)", o.NThreads)
	}
}

func TestOptions_WithDefaults_KeepAliveSecsSemantics(t *testing.T) {
	// AC-016-9 + ADR-015 point 4: 0 falls back to the 300s default;
	// negative means "never unload" and must be left untouched (not
	// mistaken for the zero-value default); a positive explicit value is
	// left untouched too.
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero falls back to 300s default", 0, 300},
		{"negative means never unload, left untouched", -1, -1},
		{"large negative left untouched", -60, -60},
		{"positive explicit value left untouched", 42, 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{KeepAliveSecs: tc.in}.WithDefaults()
			if o.KeepAliveSecs != tc.want {
				t.Fatalf("KeepAliveSecs = %d, want %d", o.KeepAliveSecs, tc.want)
			}
		})
	}
}

// TestOptions_LoadKey_IgnoresSamplerAndOutputFields is AC-016-4 at the
// pure-Go level: two Options that only differ in Temperature/TopK/TopP/
// MaxPredict/KeepAliveSecs must compare equal via loadKey(), so
// ensureLoaded's "if e.loaded == key" guard does not force a reload
// between e.g. a normal chat call and a lower-temperature summarization
// call using the same model/n_ctx/n_threads/n_batch/kv_cache_type.
func TestOptions_LoadKey_IgnoresSamplerAndOutputFields(t *testing.T) {
	base := Options{
		ModelPath:   "/models/bonsai.gguf",
		NCtx:        2048,
		NThreads:    2,
		NBatch:      256,
		KVCacheType: "q8_0",
	}

	a := base
	a.Temperature = 0.1
	a.TopK = 10
	a.TopP = 0.5
	a.MaxPredict = 32
	a.KeepAliveSecs = 5

	b := base
	b.Temperature = 0.9
	b.TopK = 40
	b.TopP = 0.95
	b.MaxPredict = 512
	b.KeepAliveSecs = -1

	if a.loadKey() != b.loadKey() {
		t.Fatalf("loadKey() differs for options that only vary in sampler/output fields:\na = %+v\nb = %+v", a.loadKey(), b.loadKey())
	}
}

// TestOptions_LoadKey_ChangesWithLoadAffectingFields is the mirror check:
// every field that DOES require a reload must actually change loadKey().
func TestOptions_LoadKey_ChangesWithLoadAffectingFields(t *testing.T) {
	base := Options{ModelPath: "/models/bonsai.gguf", NCtx: 2048, NThreads: 2, NBatch: 256, KVCacheType: "q8_0"}

	tests := []struct {
		name   string
		modify func(o Options) Options
	}{
		{"model path", func(o Options) Options { o.ModelPath = "/models/other.gguf"; return o }},
		{"n_ctx", func(o Options) Options { o.NCtx = 4096; return o }},
		{"n_threads", func(o Options) Options { o.NThreads = 4; return o }},
		{"n_batch", func(o Options) Options { o.NBatch = 512; return o }},
		{"kv_cache_type", func(o Options) Options { o.KVCacheType = "f16"; return o }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modified := tc.modify(base)
			if base.loadKey() == modified.loadKey() {
				t.Fatalf("loadKey() unchanged after modifying %s (base=%+v, modified=%+v)", tc.name, base.loadKey(), modified.loadKey())
			}
		})
	}
}

func TestOptions_LoadKey_EqualForIdenticalLoadFields(t *testing.T) {
	a := Options{ModelPath: "/m.gguf", NCtx: 2048, NThreads: 2, NBatch: 256, KVCacheType: "q8_0"}
	b := Options{ModelPath: "/m.gguf", NCtx: 2048, NThreads: 2, NBatch: 256, KVCacheType: "q8_0"}
	if a.loadKey() != b.loadKey() {
		t.Fatalf("loadKey() differs for identical load fields: %+v vs %+v", a.loadKey(), b.loadKey())
	}
}

// TestEstimateMemory_Q8_0VsF16 covers ADR-017/C3: the two KV cache types
// must produce different (and correctly ordered -- f16 costs roughly
// double q8_0 per the plan's measured qwen3-1.7B figures) KVBytes for the
// same NCtx.
func TestEstimateMemory_Q8_0VsF16(t *testing.T) {
	base := Options{ModelPath: "/does/not/exist.gguf", NCtx: 2048}

	q8 := estimateMemory(base)
	if q8.KVBytes != uint64(2048)*kvBytesPerTokenQ8_0 {
		t.Fatalf("q8_0 KVBytes = %d, want %d", q8.KVBytes, uint64(2048)*kvBytesPerTokenQ8_0)
	}

	f16opts := base
	f16opts.KVCacheType = "f16"
	f16 := estimateMemory(f16opts)
	if f16.KVBytes != uint64(2048)*kvBytesPerTokenF16 {
		t.Fatalf("f16 KVBytes = %d, want %d", f16.KVBytes, uint64(2048)*kvBytesPerTokenF16)
	}
	if f16.KVBytes <= q8.KVBytes {
		t.Fatalf("f16 KVBytes (%d) should exceed q8_0 KVBytes (%d)", f16.KVBytes, q8.KVBytes)
	}
	if q8.NCtx != 2048 || q8.KVType != "" {
		t.Fatalf("estimate NCtx/KVType = (%d, %q), want (2048, \"\")", q8.NCtx, q8.KVType)
	}
}

func TestEstimateMemory_ModelBytesFromRealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, make([]byte, 1234), 0o644); err != nil {
		t.Fatal(err)
	}
	est := estimateMemory(Options{ModelPath: path, NCtx: 1})
	if est.ModelBytes != 1234 {
		t.Fatalf("ModelBytes = %d, want 1234", est.ModelBytes)
	}
}

func TestEstimateMemory_MissingFileYieldsZeroModelBytes(t *testing.T) {
	est := estimateMemory(Options{ModelPath: "/definitely/does/not/exist.gguf", NCtx: 1})
	if est.ModelBytes != 0 {
		t.Fatalf("ModelBytes = %d, want 0 for a nonexistent file", est.ModelBytes)
	}
}

func TestMemoryHooks_NoOpUntilInstalled(t *testing.T) {
	SetMemoryHooks(nil, nil) // ensure a clean slate regardless of test order
	if err := callPreLoadHook(MemoryEstimate{}); err != nil {
		t.Fatalf("callPreLoadHook with no hook installed: %v, want nil", err)
	}
	callPostLoadHook(MemoryEstimate{}) // must not panic
}

func TestSetMemoryHooks_PreLoadCanRefuse(t *testing.T) {
	defer SetMemoryHooks(nil, nil)
	wantErr := errors.New("overloaded")
	SetMemoryHooks(func(MemoryEstimate) error { return wantErr }, nil)

	if err := callPreLoadHook(MemoryEstimate{NCtx: 2048}); !errors.Is(err, wantErr) {
		t.Fatalf("callPreLoadHook() = %v, want %v", err, wantErr)
	}
}

func TestSetMemoryHooks_PostLoadReceivesTheEstimate(t *testing.T) {
	defer SetMemoryHooks(nil, nil)
	var got MemoryEstimate
	SetMemoryHooks(nil, func(est MemoryEstimate) { got = est })

	want := MemoryEstimate{ModelBytes: 1, KVBytes: 2, NCtx: 2048, KVType: "q8_0"}
	callPostLoadHook(want)
	if got != want {
		t.Fatalf("postLoad hook received %+v, want %+v", got, want)
	}
}

func TestSetMemoryHooks_NilRemovesBothHooks(t *testing.T) {
	SetMemoryHooks(func(MemoryEstimate) error { return errors.New("should never run") }, func(MemoryEstimate) { t.Fatal("postLoad hook should never run") })
	SetMemoryHooks(nil, nil)

	if err := callPreLoadHook(MemoryEstimate{}); err != nil {
		t.Fatalf("callPreLoadHook() = %v, want nil after SetMemoryHooks(nil, nil)", err)
	}
	callPostLoadHook(MemoryEstimate{})
}
