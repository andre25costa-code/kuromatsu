//go:build linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readRSS reads the process's resident set size from /proc/self/status,
// matching the field pkg/tools/sysmon_linux.go's mem action also reads.
func readRSS() (uint64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "VmRSS:"))
		if len(fields) == 0 {
			break
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, err
		}
		return kb * 1024, nil
	}
	return 0, fmt.Errorf("VmRSS not found in /proc/self/status")
}
