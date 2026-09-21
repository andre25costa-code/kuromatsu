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

	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// Memory reading (cgroup v2/v1, falling back to /proc/meminfo) is shared
// with pkg/runstate/memguard.go via pkg/sysinfo (Trilho F Fix 7) -- this
// file used to carry its own copy of the same /proc/sys parsers.
func sysmonMemResult() *ToolResult {
	if used, limit, ok := sysinfo.CgroupMemory(); ok {
		return NewToolResult(formatMemReport("cgroup", used, limit))
	}

	total, available, err := sysinfo.MemInfo()
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
