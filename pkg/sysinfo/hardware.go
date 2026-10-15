package sysinfo

import (
	"runtime"
)

// Hardware is a snapshot of the host as Kuromatsu sees it: memory, CPU
// count and whether the CPU can run the native (llama.cpp) build's
// instruction-set baseline. Zero memory fields mean "unknown".
type Hardware struct {
	TotalRAM     uint64 // bytes; 0 = unknown
	AvailableRAM uint64 // bytes; 0 = unknown
	MemLimit     uint64 // cgroup memory limit in bytes; 0 = no limit or unknown
	CPUs         int    // runtime.NumCPU() (honours CPU affinity on Linux)
	Arch         string // runtime.GOARCH
	OS           string // runtime.GOOS

	// NativeCPUOK reports whether this CPU has every instruction-set
	// extension the native build for this architecture is compiled with;
	// NativeCPUMissing names the absent ones (e.g. ["AVX2", "FMA"]).
	NativeCPUOK      bool
	NativeCPUMissing []string
}

// Budget is the RAM Kuromatsu can actually use: the smaller of the total
// RAM and the cgroup limit, ignoring a zero (unset) limit. 0 means unknown.
func (h Hardware) Budget() uint64 {
	if h.MemLimit > 0 && (h.TotalRAM == 0 || h.MemLimit < h.TotalRAM) {
		return h.MemLimit
	}
	return h.TotalRAM
}

// Detect reads the host's hardware. It never fails: anything it cannot
// read on this platform is left at its "unknown" zero value.
//
// It is deliberately separate from MemInfo: MemInfo failing outside Linux
// is what keeps the memory guard off there, and that must not change.
func Detect() Hardware {
	hw := Hardware{
		CPUs: runtime.NumCPU(),
		Arch: runtime.GOARCH,
		OS:   runtime.GOOS,
	}
	if total, avail, err := platformMemory(); err == nil {
		hw.TotalRAM, hw.AvailableRAM = total, avail
	}
	hw.MemLimit = platformMemLimit()
	hw.NativeCPUOK, hw.NativeCPUMissing = NativeCPUFeatures()
	return hw
}
