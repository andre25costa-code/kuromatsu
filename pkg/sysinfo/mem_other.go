//go:build !linux && !android && !windows && !darwin

package sysinfo

import "errors"

// platformMemory has no reader on this platform; the RAM stays unknown.
func platformMemory() (total, avail uint64, err error) {
	return 0, 0, errors.New("sysinfo: memory detection not supported on this platform")
}

func platformMemLimit() uint64 { return 0 }
