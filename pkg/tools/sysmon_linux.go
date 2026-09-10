//go:build linux

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// cgroup v2 memory.current/memory.max are the primary source (matches the
// deploy target, S32/NFR-001): they report the container's limit, not the
// host's. cgroup v1 and /proc/meminfo are fallbacks for bare-metal/dev use.
const (
	cgroupV2CurrentPath = "/sys/fs/cgroup/memory.current"
	cgroupV2MaxPath     = "/sys/fs/cgroup/memory.max"
	cgroupV1UsagePath   = "/sys/fs/cgroup/memory/memory.usage_in_bytes"
	cgroupV1LimitPath   = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
)

func sysmonMemResult() *ToolResult {
	if used, limit, ok := readCgroupV2Memory(); ok {
		return NewToolResult(formatMemReport("cgroup v2", used, limit))
	}
	if used, limit, ok := readCgroupV1Memory(); ok {
		return NewToolResult(formatMemReport("cgroup v1", used, limit))
	}

	total, available, err := readProcMeminfoAt("/proc/meminfo")
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read memory info: %v", err))
	}
	used := total - available
	return NewToolResult(formatMemReport("host (/proc/meminfo, no cgroup limit found)", used, total))
}

func formatMemReport(source string, usedBytes, limitBytes uint64) string {
	pct := 0.0
	if limitBytes > 0 {
		pct = float64(usedBytes) / float64(limitBytes) * 100
	}
	return fmt.Sprintf("source: %s\nused: %s\nlimit: %s\nusage: %.1f%%",
		source, formatBytes(usedBytes), formatBytes(limitBytes), pct)
}

func readCgroupV2Memory() (used, limit uint64, ok bool) {
	return readCgroupV2MemoryAt(cgroupV2CurrentPath, cgroupV2MaxPath)
}

func readCgroupV2MemoryAt(currentPath, maxPath string) (used, limit uint64, ok bool) {
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
		return used, 0, true // no limit set; report 0 (0% usage shown instead of a bogus limit)
	}
	limit, err = strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return used, limit, true
}

func readCgroupV1Memory() (used, limit uint64, ok bool) {
	return readCgroupV1MemoryAt(cgroupV1UsagePath, cgroupV1LimitPath)
}

func readCgroupV1MemoryAt(usagePath, limitPath string) (used, limit uint64, ok bool) {
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
	// cgroup v1 reports an enormous sentinel (close to 2^63 or 2^64, minus a
	// page) instead of "max" when there is no limit.
	const noLimitSentinel = uint64(1) << 62
	if limit > noLimitSentinel {
		return used, 0, true
	}
	return used, limit, true
}

func readProcMeminfoAt(path string) (totalBytes, availableBytes uint64, err error) {
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
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = kb * 1024
	}
	total, hasTotal := values["MemTotal"]
	available, hasAvailable := values["MemAvailable"]
	if !hasTotal || !hasAvailable {
		return 0, 0, fmt.Errorf("MemTotal/MemAvailable not found in /proc/meminfo")
	}
	return total, available, nil
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

type sysmonProcess struct {
	pid    int
	name   string
	rssKiB uint64
}

func sysmonTopResult(limit int) *ToolResult {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read /proc: %v", err))
	}

	procs := make([]sysmonProcess, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a PID directory
		}
		name, rss, ok := readProcStatus(pid)
		if !ok {
			continue // process exited between ReadDir and here, or is unreadable
		}
		procs = append(procs, sysmonProcess{pid: pid, name: name, rssKiB: rss})
	}

	sort.Slice(procs, func(i, j int) bool { return procs[i].rssKiB > procs[j].rssKiB })
	if limit < len(procs) {
		procs = procs[:limit]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "top %d processes by RSS:\n", len(procs))
	for _, p := range procs {
		fmt.Fprintf(&b, "  pid=%-7d rss=%-10s %s\n", p.pid, formatBytes(p.rssKiB*1024), p.name)
	}
	return NewToolResult(strings.TrimRight(b.String(), "\n"))
}

func readProcStatus(pid int) (name string, rssKiB uint64, ok bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return "", 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "Name:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "VmRSS:"):
			fields := strings.Fields(strings.TrimPrefix(line, "VmRSS:"))
			if len(fields) > 0 {
				rssKiB, _ = strconv.ParseUint(fields[0], 10, 64)
			}
		}
	}
	return name, rssKiB, name != ""
}

func sysmonLoadResult() *ToolResult {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read /proc/loadavg: %v", err))
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return ErrorResult("unexpected /proc/loadavg format")
	}
	return NewToolResult(fmt.Sprintf("load average: 1m=%s 5m=%s 15m=%s", fields[0], fields[1], fields[2]))
}

func sysmonDiskResult() *ToolResult {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return ErrorResult(fmt.Sprintf("failed to stat filesystem: %v", err))
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return NewToolResult(fmt.Sprintf("path: /\nused: %s\ntotal: %s\nusage: %.1f%%",
		formatBytes(used), formatBytes(total), pct))
}

func sysmonKillResult(pid int) *ToolResult {
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return ErrorResult(fmt.Sprintf("failed to kill pid %d: %v", pid, err))
	}
	return NewToolResult(fmt.Sprintf("sent SIGKILL to pid %d", pid))
}

func sysmonReniceResult(pid, niceness int) *ToolResult {
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, niceness); err != nil {
		return ErrorResult(fmt.Sprintf("failed to renice pid %d to %d: %v", pid, niceness, err))
	}
	return NewToolResult(fmt.Sprintf("set pid %d niceness to %d", pid, niceness))
}
