package sysinfo

import (
	"reflect"
	"runtime"
	"testing"
)

func TestDetect_ReturnsCPUsAndRAM(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "android", "windows", "darwin":
	default:
		t.Skipf("memory detection not supported on %s", runtime.GOOS)
	}
	hw := Detect()
	if hw.CPUs < 1 {
		t.Fatalf("CPUs = %d, want >= 1", hw.CPUs)
	}
	if hw.TotalRAM == 0 {
		t.Fatal("TotalRAM = 0, want the host's RAM")
	}
	if hw.Arch != runtime.GOARCH || hw.OS != runtime.GOOS {
		t.Fatalf("Arch/OS = %s/%s, want %s/%s", hw.Arch, hw.OS, runtime.GOARCH, runtime.GOOS)
	}
	if runtime.GOOS == "darwin" {
		if hw.AvailableRAM != 0 {
			t.Fatalf("AvailableRAM = %d on darwin, want 0 (unknown)", hw.AvailableRAM)
		}
	} else if hw.AvailableRAM == 0 {
		t.Fatal("AvailableRAM = 0, want the host's available RAM")
	}
}

func TestBudget_UsesCgroupLimit(t *testing.T) {
	const gib = uint64(1) << 30
	cases := []struct {
		name string
		hw   Hardware
		want uint64
	}{
		{"no limit", Hardware{TotalRAM: 16 * gib}, 16 * gib},
		{"limit below total", Hardware{TotalRAM: 16 * gib, MemLimit: gib}, gib},
		{"limit above total", Hardware{TotalRAM: 2 * gib, MemLimit: 8 * gib}, 2 * gib},
		{"total unknown", Hardware{MemLimit: gib}, gib},
		{"all unknown", Hardware{}, 0},
	}
	for _, tc := range cases {
		if got := tc.hw.Budget(); got != tc.want {
			t.Errorf("%s: Budget() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestNativeCPUFeaturesFor(t *testing.T) {
	cases := []struct {
		arch                     string
		avx2, fma, dotprod, fp16 bool
		wantOK                   bool
		wantMissing              []string
	}{
		{"amd64", true, true, false, false, true, nil},
		{"amd64", false, false, true, true, false, []string{"AVX2", "FMA"}},
		{"amd64", true, false, false, false, false, []string{"FMA"}},
		{"arm64", false, false, true, true, true, nil},
		{"arm64", true, true, false, true, false, []string{"dotprod"}},
		{"arm64", false, false, true, false, false, []string{"fp16"}},
		{"riscv64", true, true, true, true, false, []string{"arch"}},
	}
	for _, tc := range cases {
		ok, missing := nativeCPUFeaturesFor(tc.arch, tc.avx2, tc.fma, tc.dotprod, tc.fp16)
		if ok != tc.wantOK || !reflect.DeepEqual(missing, tc.wantMissing) {
			t.Errorf("%s avx2=%v fma=%v dotprod=%v fp16=%v: got (%v, %v), want (%v, %v)",
				tc.arch, tc.avx2, tc.fma, tc.dotprod, tc.fp16, ok, missing, tc.wantOK, tc.wantMissing)
		}
	}
}
