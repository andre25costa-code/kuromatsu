package localllm

import (
	"fmt"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// cpuSupported is a seam over sysinfo.NativeCPUFeatures for tests.
var cpuSupported = sysinfo.NativeCPUFeatures

// CPUSupported reports whether this CPU has the instruction-set extensions
// the native build is compiled with (AVX2+FMA on amd64, dotprod+fp16 on
// arm64), and which ones are missing. Running ggml code on a CPU without
// them dies with SIGILL, so the native model must not be loaded then.
func CPUSupported() (ok bool, missing []string) {
	return cpuSupported()
}

// errCPUUnsupported is the error a load attempt returns on such a CPU.
func errCPUUnsupported(missing []string) error {
	return fmt.Errorf(
		"localllm: this native build needs %s, which this CPU lacks; use a remote model or the pure-Go release",
		strings.Join(missing, "+"))
}
