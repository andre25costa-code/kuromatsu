//go:build windows

package sysinfo

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX struct, which
// golang.org/x/sys/windows does not define.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// platformMemory calls GlobalMemoryStatusEx. The available figure includes
// the reclaimable standby cache, so it is for display only.
func platformMemory() (total, avail uint64, err error) {
	// LazyProc.Call panics when the procedure cannot be resolved.
	if err := procGlobalMemoryStatusEx.Find(); err != nil {
		return 0, 0, err
	}
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 0, 0, callErr
	}
	return m.TotalPhys, m.AvailPhys, nil
}

func platformMemLimit() uint64 { return 0 }
