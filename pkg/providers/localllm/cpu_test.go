package localllm

import (
	"strings"
	"testing"
)

func TestCPUSupported_UsesSeamAndNamesMissing(t *testing.T) {
	old := cpuSupported
	t.Cleanup(func() { cpuSupported = old })
	cpuSupported = func() (bool, []string) { return false, []string{"AVX2", "FMA"} }

	ok, missing := CPUSupported()
	if ok || len(missing) != 2 {
		t.Fatalf("CPUSupported = %v, %v", ok, missing)
	}
	msg := errCPUUnsupported(missing).Error()
	for _, want := range []string{"needs AVX2+FMA", "use a remote model or the pure-Go release"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
}
