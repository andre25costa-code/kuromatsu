//go:build linux

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadCgroupV2MemoryAt_WithLimit(t *testing.T) {
	dir := t.TempDir()
	current := writeTestFile(t, dir, "memory.current", "104857600\n") // 100 MiB
	max := writeTestFile(t, dir, "memory.max", "943718400\n")        // 900 MiB

	used, limit, ok := readCgroupV2MemoryAt(current, max)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if used != 104857600 || limit != 943718400 {
		t.Fatalf("used=%d limit=%d", used, limit)
	}
}

func TestReadCgroupV2MemoryAt_Unlimited(t *testing.T) {
	dir := t.TempDir()
	current := writeTestFile(t, dir, "memory.current", "1000\n")
	max := writeTestFile(t, dir, "memory.max", "max\n")

	used, limit, ok := readCgroupV2MemoryAt(current, max)
	if !ok || used != 1000 || limit != 0 {
		t.Fatalf("used=%d limit=%d ok=%v", used, limit, ok)
	}
}

func TestReadCgroupV2MemoryAt_MissingFile(t *testing.T) {
	_, _, ok := readCgroupV2MemoryAt("/does/not/exist/current", "/does/not/exist/max")
	if ok {
		t.Fatal("expected ok=false when the cgroup files don't exist")
	}
}

func TestReadCgroupV1MemoryAt_NoLimitSentinel(t *testing.T) {
	dir := t.TempDir()
	usage := writeTestFile(t, dir, "usage_in_bytes", "2000\n")
	// cgroup v1's "unlimited" sentinel, e.g. 9223372036854771712.
	limitPath := writeTestFile(t, dir, "limit_in_bytes", strconv.FormatUint(1<<63-4096, 10)+"\n")

	used, limit, ok := readCgroupV1MemoryAt(usage, limitPath)
	if !ok || used != 2000 || limit != 0 {
		t.Fatalf("used=%d limit=%d ok=%v", used, limit, ok)
	}
}

func TestReadProcMeminfoAt(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "meminfo", strings.Join([]string{
		"MemTotal:        1000000 kB",
		"MemFree:          200000 kB",
		"MemAvailable:     500000 kB",
		"Buffers:           10000 kB",
	}, "\n")+"\n")

	total, available, err := readProcMeminfoAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1000000*1024 || available != 500000*1024 {
		t.Fatalf("total=%d available=%d", total, available)
	}
}

func TestReadProcMeminfoAt_MissingFields(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "meminfo", "SomeOtherField: 1 kB\n")
	if _, _, err := readProcMeminfoAt(path); err == nil {
		t.Fatal("expected an error when MemTotal/MemAvailable are absent")
	}
}

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
