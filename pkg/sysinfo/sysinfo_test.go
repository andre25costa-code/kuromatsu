package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMemInfoAt_ParsesTotalAndAvailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	writeFile(t, path, "MemTotal:        1017152 kB\nMemFree:          200000 kB\nMemAvailable:     466000 kB\n")

	total, available, err := memInfoAt(path)
	if err != nil {
		t.Fatalf("memInfoAt: %v", err)
	}
	if total != 1017152*1024 {
		t.Fatalf("total = %d, want %d", total, 1017152*1024)
	}
	if available != 466000*1024 {
		t.Fatalf("available = %d, want %d", available, 466000*1024)
	}
}

func TestMemInfoAt_MissingFileErrors(t *testing.T) {
	if _, _, err := memInfoAt(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("memInfoAt(missing file) = nil error, want an error")
	}
}

func TestMemInfoAt_MissingFieldsErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	writeFile(t, path, "MemFree: 1 kB\n")
	if _, _, err := memInfoAt(path); err == nil {
		t.Fatal("memInfoAt(no MemTotal/MemAvailable) = nil error, want an error")
	}
}

func TestCgroupV2MemoryAt_NoLimitReportsZero(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "memory.current")
	max := filepath.Join(dir, "memory.max")
	writeFile(t, current, "104857600\n")
	writeFile(t, max, "max\n")

	used, limit, ok := cgroupV2MemoryAt(current, max)
	if !ok {
		t.Fatal("cgroupV2MemoryAt ok = false, want true")
	}
	if used != 104857600 {
		t.Fatalf("used = %d, want 104857600", used)
	}
	if limit != 0 {
		t.Fatalf("limit = %d, want 0 (no limit set)", limit)
	}
}

func TestCgroupV2MemoryAt_WithLimit(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "memory.current")
	max := filepath.Join(dir, "memory.max")
	writeFile(t, current, "50\n")
	writeFile(t, max, "1000\n")

	used, limit, ok := cgroupV2MemoryAt(current, max)
	if !ok || used != 50 || limit != 1000 {
		t.Fatalf("cgroupV2MemoryAt = (%d, %d, %v), want (50, 1000, true)", used, limit, ok)
	}
}

func TestCgroupV2MemoryAt_MissingFilesNotOK(t *testing.T) {
	dir := t.TempDir()
	if _, _, ok := cgroupV2MemoryAt(filepath.Join(dir, "nope1"), filepath.Join(dir, "nope2")); ok {
		t.Fatal("cgroupV2MemoryAt(missing files) ok = true, want false")
	}
}

func TestCgroupV1MemoryAt_SentinelMeansNoLimit(t *testing.T) {
	dir := t.TempDir()
	usage := filepath.Join(dir, "usage_in_bytes")
	limitFile := filepath.Join(dir, "limit_in_bytes")
	writeFile(t, usage, "12345\n")
	writeFile(t, limitFile, "9223372036854771712\n") // the real no-limit sentinel value

	used, limit, ok := cgroupV1MemoryAt(usage, limitFile)
	if !ok || used != 12345 || limit != 0 {
		t.Fatalf("cgroupV1MemoryAt = (%d, %d, %v), want (12345, 0, true)", used, limit, ok)
	}
}

func TestPsiAt_ParsesSomeAndFull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory")
	writeFile(t, path, "some avg10=12.34 avg60=5.00 avg300=1.00 total=1000\nfull avg10=3.21 avg60=1.00 avg300=0.50 total=500\n")

	p, err := psiAt(path)
	if err != nil {
		t.Fatalf("psiAt: %v", err)
	}
	if p.SomeAvg10 != 12.34 || p.SomeAvg60 != 5.00 || p.SomeAvg300 != 1.00 {
		t.Fatalf("some = %+v, want {12.34 5.00 1.00}", p)
	}
	if p.FullAvg10 != 3.21 || p.FullAvg60 != 1.00 || p.FullAvg300 != 0.50 {
		t.Fatalf("full = %+v, want {3.21 1.00 0.50}", p)
	}
}

func TestPsiAt_MissingFileErrors(t *testing.T) {
	if _, err := psiAt(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("psiAt(missing file) = nil error, want an error")
	}
}

func TestCpuStatAt_ParsesAggregateLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat")
	writeFile(t, path, "cpu  100 10 50 800 5 0 2 30 0 0\ncpu0 50 5 25 400 2 0 1 15 0 0\n")

	stat, err := cpuStatAt(path)
	if err != nil {
		t.Fatalf("cpuStatAt: %v", err)
	}
	want := CPUStat{User: 100, Nice: 10, System: 50, Idle: 800, Iowait: 5, IRQ: 0, SoftIRQ: 2, Steal: 30}
	if stat != want {
		t.Fatalf("cpuStatAt = %+v, want %+v", stat, want)
	}
}

func TestCpuStatAt_MissingFileErrors(t *testing.T) {
	if _, err := cpuStatAt(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("cpuStatAt(missing file) = nil error, want an error")
	}
}

func TestCpuStatAt_NoCpuLineErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat")
	writeFile(t, path, "intr 12345\n")
	if _, err := cpuStatAt(path); err == nil {
		t.Fatal("cpuStatAt(no cpu line) = nil error, want an error")
	}
}

func TestCPUStat_StealPercent(t *testing.T) {
	start := CPUStat{User: 100, Idle: 800, Steal: 10}
	end := CPUStat{User: 150, Idle: 800, Steal: 60} // +50 user, +50 steal -> 50% of the +100 delta

	got := end.StealPercent(start)
	if got != 50 {
		t.Fatalf("StealPercent() = %v, want 50", got)
	}
}

func TestCPUStat_StealPercent_NoElapsedTimeReturnsZero(t *testing.T) {
	same := CPUStat{User: 100, Idle: 800, Steal: 10}
	if got := same.StealPercent(same); got != 0 {
		t.Fatalf("StealPercent() = %v, want 0 when total is unchanged", got)
	}
}

func TestProcessRSSAt_ParsesVmRSS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status")
	writeFile(t, path, "Name:\ttest\nVmRSS:\t    2048 kB\n")

	rss, ok := processRSSAt(path)
	if !ok || rss != 2048*1024 {
		t.Fatalf("processRSSAt = (%d, %v), want (%d, true)", rss, ok, 2048*1024)
	}
}

func TestProcessRSSAt_MissingFileNotOK(t *testing.T) {
	if _, ok := processRSSAt(filepath.Join(t.TempDir(), "nope")); ok {
		t.Fatal("processRSSAt(missing file) ok = true, want false")
	}
}

func TestProcessRSSAt_NoVmRSSLineNotOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status")
	writeFile(t, path, "Name:\ttest\n")
	if _, ok := processRSSAt(path); ok {
		t.Fatal("processRSSAt(no VmRSS line) ok = true, want false")
	}
}
