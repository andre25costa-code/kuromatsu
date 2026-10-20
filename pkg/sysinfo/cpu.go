package sysinfo

import (
	"runtime"

	"golang.org/x/sys/cpu"
)

// NativeCPUFeatures reports whether this CPU has the instruction-set
// baseline the native (llama.cpp) build is compiled with, and which
// extensions are missing.
//
//   - amd64: the build enables AVX, AVX2, FMA and F16C. x/sys/cpu does not
//     expose F16C, but every x86 CPU shipped with both AVX2 and FMA also has
//     F16C, so AVX2+FMA is the check.
//   - arm64: the build targets armv8.2-a+dotprod+fp16, so it needs the
//     dot-product extension and half-precision arithmetic (either flag).
//   - any other architecture has no native build: missing is ["arch"].
func NativeCPUFeatures() (ok bool, missing []string) {
	return nativeCPUFeaturesFor(runtime.GOARCH, cpu.X86.HasAVX2, cpu.X86.HasFMA,
		cpu.ARM64.HasASIMDDP, cpu.ARM64.HasASIMDHP || cpu.ARM64.HasFPHP)
}

func nativeCPUFeaturesFor(arch string, avx2, fma, dotprod, fp16 bool) (bool, []string) {
	var missing []string
	switch arch {
	case "amd64":
		if !avx2 {
			missing = append(missing, "AVX2")
		}
		if !fma {
			missing = append(missing, "FMA")
		}
	case "arm64":
		if !dotprod {
			missing = append(missing, "dotprod")
		}
		if !fp16 {
			missing = append(missing, "fp16")
		}
	default:
		missing = []string{"arch"}
	}
	return len(missing) == 0, missing
}
