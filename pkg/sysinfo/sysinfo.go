// Package sysinfo reads host/cgroup memory and PSI pressure straight from
// /proc and /sys/fs/cgroup, shared by pkg/runstate/memguard.go (ADR-017)
// and, historically, pkg/tools/sysmon_linux.go's own memory readers.
//
// Deliberately no build tags: every function here is a plain os.ReadFile
// against a /proc or /sys/fs/cgroup path, which simply fails (a returned
// error or ok=false, never a compile error or a panic) on a platform that
// doesn't have it -- e.g. Windows. That is exactly the behavior
// pkg/runstate/memguard.go needs for AC-018-6 ("sem Linux/PSI: memguard
// desliga com aviso"): callers detect "not available" from an ordinary
// error at runtime, so no platform-specific file/build tag is needed in
// this package OR in memguard.go.
package sysinfo

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MemInfo reads /proc/meminfo's MemTotal/MemAvailable, in bytes.
func MemInfo() (totalBytes, availableBytes uint64, err error) {
	return memInfoAt("/proc/meminfo")
}

func memInfoAt(path string) (totalBytes, availableBytes uint64, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}
		kb, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		values[key] = kb * 1024
	}
	total, hasTotal := values["MemTotal"]
	available, hasAvailable := values["MemAvailable"]
	if !hasTotal || !hasAvailable {
		return 0, 0, fmt.Errorf("sysinfo: MemTotal/MemAvailable not found in %s", path)
	}
	return total, available, nil
}

// CgroupMemory reads cgroup v2 memory.current/memory.max, falling back to
// cgroup v1. ok is false when neither is readable -- not an error, since
// "no cgroup" (bare host, most non-Linux platforms) is a normal case, not
// a failure.
func CgroupMemory() (used, limit uint64, ok bool) {
	if used, limit, ok := cgroupV2MemoryAt("/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory.max"); ok {
		return used, limit, true
	}
	return cgroupV1MemoryAt("/sys/fs/cgroup/memory/memory.usage_in_bytes", "/sys/fs/cgroup/memory/memory.limit_in_bytes")
}

func cgroupV2MemoryAt(currentPath, maxPath string) (used, limit uint64, ok bool) {
	usedStr, err := os.ReadFile(currentPath)
	if err != nil {
		return 0, 0, false
	}
	used, err = strconv.ParseUint(strings.TrimSpace(string(usedStr)), 10, 64)
	if err != nil {
		return 0, 0, false
	}

	limitStr, err := os.ReadFile(maxPath)
	if err != nil {
		return 0, 0, false
	}
	trimmed := strings.TrimSpace(string(limitStr))
	if trimmed == "max" {
		return used, 0, true // no limit set; 0 means "unbounded" to callers
	}
	limit, err = strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return used, limit, true
}

func cgroupV1MemoryAt(usagePath, limitPath string) (used, limit uint64, ok bool) {
	usedStr, err := os.ReadFile(usagePath)
	if err != nil {
		return 0, 0, false
	}
	used, err = strconv.ParseUint(strings.TrimSpace(string(usedStr)), 10, 64)
	if err != nil {
		return 0, 0, false
	}

	limitStr, err := os.ReadFile(limitPath)
	if err != nil {
		return 0, 0, false
	}
	limit, err = strconv.ParseUint(strings.TrimSpace(string(limitStr)), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	// cgroup v1 reports an enormous sentinel instead of "max" when there
	// is no limit (same convention pkg/tools/sysmon_linux.go's own
	// cgroup v1 reader already relies on).
	const noLimitSentinel = uint64(1) << 62
	if limit > noLimitSentinel {
		return used, 0, true
	}
	return used, limit, true
}

// Pressure is one /proc/pressure/<resource> snapshot (avg10/avg60/avg300,
// as whole percentages, matching the kernel's own units).
type Pressure struct {
	SomeAvg10, SomeAvg60, SomeAvg300 float64
	FullAvg10, FullAvg60, FullAvg300 float64
}

// PSI reads /proc/pressure/<resource> (e.g. "memory"). Returns an error on
// any platform/kernel without PSI (Windows, or Linux <4.20 / PSI disabled
// in the running kernel) -- AC-018-6.
func PSI(resource string) (Pressure, error) {
	return psiAt("/proc/pressure/" + resource)
}

func psiAt(path string) (Pressure, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Pressure{}, err
	}
	var p Pressure
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		kind := fields[0]
		avgs := make(map[string]float64, 4)
		for _, kv := range fields[1:] {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			f, perr := strconv.ParseFloat(v, 64)
			if perr != nil {
				continue
			}
			avgs[k] = f
		}
		switch kind {
		case "some":
			p.SomeAvg10, p.SomeAvg60, p.SomeAvg300 = avgs["avg10"], avgs["avg60"], avgs["avg300"]
		case "full":
			p.FullAvg10, p.FullAvg60, p.FullAvg300 = avgs["avg10"], avgs["avg60"], avgs["avg300"]
		}
	}
	return p, nil
}

// CPUStat is the aggregate ("cpu ", all cores) line of /proc/stat, in
// jiffies. Steal is the field pkg/runstate/mode.go's Throttled bit and
// telemetry's steal_pct column both key off of (S06/R7, S34).
type CPUStat struct {
	User, Nice, System, Idle, Iowait, IRQ, SoftIRQ, Steal uint64
}

// Total sums every field -- the denominator StealPercent (and any other
// "share of CPU time" computation) divides by.
func (s CPUStat) Total() uint64 {
	return s.User + s.Nice + s.System + s.Idle + s.Iowait + s.IRQ + s.SoftIRQ + s.Steal
}

// StealPercent returns 100*deltaSteal/deltaTotal between two CPUStat
// samples (start, typically at a turn's beginning, and s, at its end).
// Returns 0 if the total didn't advance (e.g. two samples taken at
// effectively the same instant) rather than dividing by zero.
func (s CPUStat) StealPercent(start CPUStat) float64 {
	deltaTotal := s.Total() - start.Total()
	if deltaTotal == 0 {
		return 0
	}
	deltaSteal := s.Steal - start.Steal
	return 100 * float64(deltaSteal) / float64(deltaTotal)
}

// CPUStatTotal reads the aggregate CPU line from /proc/stat.
func CPUStatTotal() (CPUStat, error) {
	return cpuStatAt("/proc/stat")
}

func cpuStatAt(path string) (CPUStat, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CPUStat{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		get := func(i int) uint64 {
			if i >= len(fields) {
				return 0
			}
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			return v
		}
		return CPUStat{
			User: get(0), Nice: get(1), System: get(2), Idle: get(3),
			Iowait: get(4), IRQ: get(5), SoftIRQ: get(6), Steal: get(7),
		}, nil
	}
	return CPUStat{}, fmt.Errorf("sysinfo: no aggregate \"cpu \" line found in %s", path)
}

// ProcessRSS reads a process's resident set size (VmRSS), in bytes, from
// /proc/<pid>/status. ok is false when unreadable (process gone, or a
// platform without /proc).
func ProcessRSS(pid int) (rssBytes uint64, ok bool) {
	return processRSSAt(fmt.Sprintf("/proc/%d/status", pid))
}

func processRSSAt(path string) (rssBytes uint64, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "VmRSS:"))
		if len(fields) == 0 {
			return 0, false
		}
		kb, perr := strconv.ParseUint(fields[0], 10, 64)
		if perr != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}
