//go:build linux

package tools

import (
	"context"
	"strings"
	"testing"
)

// The cgroup v2/v1 and /proc/meminfo parsers this file used to test
// directly (readCgroupV2MemoryAt, readCgroupV1MemoryAt, readProcMeminfoAt)
// were removed in favor of pkg/sysinfo (Trilho F Fix 7, dedup) -- their
// fixture-based coverage now lives in pkg/sysinfo/sysinfo_test.go
// (TestCgroupV2MemoryAt_*, TestCgroupV1MemoryAt_*, TestMemInfoAt_*).
// What remains here is the "mem" action's own behavior, covered below by
// the integration check against the real environment.

// Light integration checks against the real environment (this test runs
// under WSL/Linux): assert the public actions succeed and produce
// sane-looking output, without asserting exact values that would be
// environment-dependent.

func TestSysmonTool_Mem_Integration(t *testing.T) {
	res := NewSysmonTool(false).Execute(context.Background(), map[string]any{"action": "mem"})
	if res.IsError {
		t.Fatalf("mem action failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "used:") || !strings.Contains(res.ForLLM, "limit:") {
		t.Fatalf("unexpected mem report shape: %s", res.ForLLM)
	}
}

// AC-011-3
func TestSysmonTool_Top_Integration(t *testing.T) {
	res := NewSysmonTool(false).Execute(context.Background(), map[string]any{"action": "top", "limit": 3})
	if res.IsError {
		t.Fatalf("top action failed: %s", res.ForLLM)
	}
	lines := strings.Split(strings.TrimSpace(res.ForLLM), "\n")
	// header line + up to 3 process lines
	if len(lines) < 2 || len(lines) > 4 {
		t.Fatalf("expected 2-4 lines (header + <=3 processes), got %d: %s", len(lines), res.ForLLM)
	}
}

func TestSysmonTool_Load_Integration(t *testing.T) {
	res := NewSysmonTool(false).Execute(context.Background(), map[string]any{"action": "load"})
	if res.IsError {
		t.Fatalf("load action failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "1m=") {
		t.Fatalf("unexpected load report shape: %s", res.ForLLM)
	}
}

func TestSysmonTool_Disk_Integration(t *testing.T) {
	res := NewSysmonTool(false).Execute(context.Background(), map[string]any{"action": "disk"})
	if res.IsError {
		t.Fatalf("disk action failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "total:") {
		t.Fatalf("unexpected disk report shape: %s", res.ForLLM)
	}
}

// A cgroup without MemoryMax= reports memory.max = "max" (limit 0). The
// usage must still come from the cgroup, but the limit falls back to the
// host's MemTotal -- otherwise the report shows "limit: 0 B, usage: 0%".
func TestMemReport_UnboundedCgroupFallsBackToHostTotal(t *testing.T) {
	res := memReport(
		func() (uint64, uint64, bool) { return 400 << 20, 0, true },
		func() (uint64, uint64, error) { return 1000 << 20, 500 << 20, nil },
	)
	if res.IsError {
		t.Fatalf("memReport() error: %s", res.ForLLM)
	}
	for _, want := range []string{"cgroup", "host MemTotal", "used: 400.0 MiB", "limit: 1000.0 MiB", "usage: 40.0%"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Fatalf("memReport() = %q, missing %q", res.ForLLM, want)
		}
	}
}

func TestMemReport_BoundedCgroupUsesItsOwnLimit(t *testing.T) {
	res := memReport(
		func() (uint64, uint64, bool) { return 450 << 20, 900 << 20, true },
		func() (uint64, uint64, error) { return 1000 << 20, 500 << 20, nil },
	)
	if !strings.Contains(res.ForLLM, "limit: 900.0 MiB") || !strings.Contains(res.ForLLM, "usage: 50.0%") ||
		strings.Contains(res.ForLLM, "host MemTotal") {
		t.Fatalf("memReport() = %q, want the cgroup's own 900 MiB limit", res.ForLLM)
	}
}

func TestMemReport_NoCgroupUsesHost(t *testing.T) {
	res := memReport(
		func() (uint64, uint64, bool) { return 0, 0, false },
		func() (uint64, uint64, error) { return 1000 << 20, 250 << 20, nil },
	)
	if !strings.Contains(res.ForLLM, "no cgroup limit found") || !strings.Contains(res.ForLLM, "usage: 75.0%") {
		t.Fatalf("memReport() = %q, want host-based report", res.ForLLM)
	}
}
