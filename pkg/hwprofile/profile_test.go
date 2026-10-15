package hwprofile

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

const (
	mib = uint64(1) << 20
	gib = uint64(1) << 30
)

func hwWith(total uint64, cpus int) sysinfo.Hardware {
	return sysinfo.Hardware{TotalRAM: total, CPUs: cpus, NativeCPUOK: true}
}

func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv(config.EnvHome, t.TempDir())
	return config.DefaultConfig()
}

func toMap(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSelect_Bands(t *testing.T) {
	cases := []struct {
		name string
		hw   sysinfo.Hardware
		want Name
	}{
		{"512MiB/1", hwWith(512*mib, 1), UltraLight},
		{"990MiB/2", hwWith(990*mib, 2), Light},
		{"4GiB/2", hwWith(4*gib, 2), Balanced},
		{"16GiB/2", hwWith(16*gib, 2), Balanced},
		{"16GiB/8", hwWith(16*gib, 8), Performance},
		{"16GiB total, 1GiB cgroup", sysinfo.Hardware{TotalRAM: 16 * gib, MemLimit: gib, CPUs: 8}, Light},
		{"RAM unknown", hwWith(0, 8), Light},
		{"767MiB", hwWith(767*mib, 4), UltraLight},
		{"768MiB", hwWith(768*mib, 4), Light},
		{"2048MiB", hwWith(2048*mib, 4), Balanced},
		{"8192MiB/4", hwWith(8192*mib, 4), Performance},
	}
	for _, tc := range cases {
		if got := Select(tc.hw); got != tc.want {
			t.Errorf("%s: Select = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestKnobsFor_Table(t *testing.T) {
	hw := hwWith(16*gib, 12)
	want := map[Name]Knobs{
		UltraLight:  {1024, 2, 128, 64, 2, 2, 2, 60},
		Light:       {2048, 4, 256, 96, 3, 5, 8, 30},
		Balanced:    {4096, 4, 512, 96, 3, 5, 8, 30},
		Performance: {8192, 8, 1024, 128, 3, 8, 16, 15},
	}
	for n, k := range want {
		if got := KnobsFor(n, hw); got != k {
			t.Errorf("KnobsFor(%s) = %+v, want %+v", n, got, k)
		}
	}
	if got := KnobsFor(Performance, hwWith(16*gib, 1)).NThreads; got != 1 {
		t.Errorf("Performance threads on 1 CPU = %d, want 1", got)
	}
}

func TestApply_LightEqualsDefaults(t *testing.T) {
	want := defaultConfig(t)
	want.Agents.Defaults.SubTurn.MaxDepth = 3
	want.Agents.Defaults.SubTurn.MaxConcurrent = 5
	want.Agents.Defaults.SubTurn.MaxConcurrentGlobal = 8

	got := config.DefaultConfig()
	hw := hwWith(990*mib, 2)
	if w := Apply(got, Light, hw); len(w) != 0 {
		t.Fatalf("warnings = %v", w)
	}

	if !reflect.DeepEqual(toMap(t, got), toMap(t, want)) {
		gj, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("Light changed the default config:\n%s", gj)
	}
	for _, m := range got.ModelList {
		if m.ModelName == config.NativeModelName {
			if _, ok := m.ExtraBody["n_threads"]; ok {
				t.Fatal("Light wrote n_threads; it must stay absent when equal to the runtime default")
			}
		}
	}
	if n := Match(config.DefaultConfig(), hw); n != Light {
		t.Fatalf("Match(DefaultConfig) = %s, want Light", n)
	}
}

// stripProfileKeys removes every key a profile may write, so what is left
// must be identical before and after Apply.
func stripProfileKeys(t *testing.T, m map[string]any) {
	t.Helper()
	for _, raw := range m["model_list"].([]any) {
		entry := raw.(map[string]any)
		if entry["model_name"] == config.NativeModelName {
			if eb, ok := entry["extra_body"].(map[string]any); ok {
				delete(eb, "n_ctx")
				delete(eb, "n_threads")
			}
		}
	}
	if mg, ok := m["memguard"].(map[string]any); ok {
		delete(mg, "min_available_mb")
		delete(mg, "margin_mb")
	}
	st := m["agents"].(map[string]any)["defaults"].(map[string]any)["subturn"].(map[string]any)
	delete(st, "max_depth")
	delete(st, "max_concurrent")
	delete(st, "max_concurrent_global")
	delete(m["heartbeat"].(map[string]any), "interval")
}

func TestApply_TouchesOnlyProfileKeys(t *testing.T) {
	for _, n := range All {
		for _, hw := range []sysinfo.Hardware{hwWith(16*gib, 12), hwWith(512*mib, 1)} {
			base := defaultConfig(t)
			base.Memguard.Enabled = true
			base.Heartbeat.Enabled = false
			before := toMap(t, base)

			Apply(base, n, hw)
			if !base.Memguard.Enabled || base.Heartbeat.Enabled {
				t.Fatalf("%s: Apply changed memguard.enabled or heartbeat.enabled", n)
			}
			after := toMap(t, base)
			stripProfileKeys(t, before)
			stripProfileKeys(t, after)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("%s on %d CPUs changed keys outside the profile", n, hw.CPUs)
			}
		}
	}
}

func TestApply_WritesProfileValues(t *testing.T) {
	cfg := defaultConfig(t)
	Apply(cfg, UltraLight, hwWith(512*mib, 4))
	native := nativeEntry(cfg)
	if native.ExtraBody["n_ctx"] != 1024 || native.ExtraBody["n_threads"] != 2 {
		t.Fatalf("native extra_body = %v, want n_ctx 1024, n_threads 2", native.ExtraBody)
	}
	if cfg.Memguard.MinAvailableMB != 128 || cfg.Memguard.MarginMB != 64 {
		t.Fatalf("memguard = %+v", cfg.Memguard)
	}
	st := cfg.Agents.Defaults.SubTurn
	if st.MaxDepth != 2 || st.MaxConcurrent != 2 || st.MaxConcurrentGlobal != 2 || cfg.Heartbeat.Interval != 60 {
		t.Fatalf("subturn = %+v, heartbeat = %d", st, cfg.Heartbeat.Interval)
	}

	// Going back to Light restores the default shape: n_threads removed,
	// memguard limits unset.
	Apply(cfg, Light, hwWith(512*mib, 4))
	if _, ok := native.ExtraBody["n_threads"]; ok || cfg.Memguard.MinAvailableMB != 0 || cfg.Memguard.MarginMB != 0 {
		t.Fatalf("Light after Ultra-Light: extra_body %v, memguard %+v", native.ExtraBody, cfg.Memguard)
	}
}

func TestApply_NoNativeEntry(t *testing.T) {
	cfg := defaultConfig(t)
	kept := cfg.ModelList[:0]
	for _, m := range cfg.ModelList {
		if m.ModelName != config.NativeModelName {
			kept = append(kept, m)
		}
	}
	cfg.ModelList = kept
	n := len(cfg.ModelList)

	Apply(cfg, UltraLight, hwWith(512*mib, 4))
	if len(cfg.ModelList) != n || nativeEntry(cfg) != nil {
		t.Fatal("Apply created the native entry")
	}
	if got := Match(cfg, hwWith(512*mib, 4)); got != UltraLight {
		t.Fatalf("Match without native entry = %s, want Ultra-Light", got)
	}
}

func TestApply_WarnsOnUnsupportedNativeCPU(t *testing.T) {
	old := nativeBuilt
	t.Cleanup(func() { nativeBuilt = old })
	hw := sysinfo.Hardware{TotalRAM: 4 * gib, CPUs: 2, NativeCPUMissing: []string{"AVX2", "FMA"}}

	nativeBuilt = func() bool { return true }
	cfg := defaultConfig(t)
	models := len(cfg.ModelList)
	w := Apply(cfg, Balanced, hw)
	if len(w) != 1 || !strings.Contains(w[0], "AVX2+FMA") {
		t.Fatalf("warnings = %v, want one naming AVX2+FMA", w)
	}
	if len(cfg.ModelList) != models {
		t.Fatal("Apply changed the model list")
	}

	nativeBuilt = func() bool { return false }
	if w := Apply(defaultConfig(t), Balanced, hw); len(w) != 0 {
		t.Fatalf("pure-Go build warned: %v", w)
	}
}

func TestApply_UnknownProfile(t *testing.T) {
	cfg := defaultConfig(t)
	before := toMap(t, cfg)
	if w := Apply(cfg, Custom, hwWith(4*gib, 2)); len(w) != 1 {
		t.Fatalf("warnings = %v", w)
	}
	if !reflect.DeepEqual(before, toMap(t, cfg)) {
		t.Fatal("unknown profile changed the config")
	}
}

func TestMatch_CustomWhenEdited(t *testing.T) {
	hw := hwWith(16*gib, 12)
	for _, n := range All {
		cfg := defaultConfig(t)
		Apply(cfg, n, hw)

		// Round-trip through the file, as `status` sees it: numbers in
		// extra_body come back as float64.
		path := filepath.Join(t.TempDir(), "config.json")
		if err := config.SaveConfig(path, cfg); err != nil {
			t.Fatal(err)
		}
		loaded, err := config.LoadConfigForEdit(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := Match(loaded, hw); got != n {
			t.Fatalf("Match after Apply(%s) = %s", n, got)
		}

		loaded.Heartbeat.Interval = 45
		if got := Match(loaded, hw); got != Custom {
			t.Fatalf("Match after editing heartbeat = %s, want custom", got)
		}
	}
}

func TestParseName(t *testing.T) {
	for in, want := range map[string]Name{
		"ultra-light": UltraLight, "LIGHT": Light, "balanced": Balanced, "Performance": Performance,
	} {
		if got, ok := ParseName(in); !ok || got != want {
			t.Errorf("ParseName(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"custom", "auto", ""} {
		if _, ok := ParseName(in); ok {
			t.Errorf("ParseName(%q) accepted", in)
		}
	}
}
