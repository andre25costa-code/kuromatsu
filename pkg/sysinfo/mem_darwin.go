//go:build darwin

package sysinfo

import "golang.org/x/sys/unix"

// platformMemory reads the total RAM from sysctl hw.memsize. The available
// RAM has no simple sysctl without cgo, so it is reported as unknown (0).
func platformMemory() (total, avail uint64, err error) {
	total, err = unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, 0, err
	}
	return total, 0, nil
}

func platformMemLimit() uint64 { return 0 }
