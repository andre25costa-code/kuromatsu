package sleep

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeSessionSource struct {
	digests    []SessionDigest
	err        error
	calls      int
	lastSince  time.Time
	lastDeep   bool
	lastWSpace string
}

func (f *fakeSessionSource) RecentDigests(_ context.Context, workspace string, since time.Time, weeklyDeep bool) ([]SessionDigest, error) {
	f.calls++
	f.lastSince = since
	f.lastDeep = weeklyDeep
	f.lastWSpace = workspace
	return f.digests, f.err
}

func newRuntimeForTest(cfg Config, sessions SessionSource, chat ChatFunc) *Runtime {
	r := NewRuntime(cfg, sessions, chat)
	return r
}

func TestRuntime_Disabled_IsNoop(t *testing.T) {
	workspace := t.TempDir()
	sessions := &fakeSessionSource{digests: []SessionDigest{{SessionID: "s1", Summary: "x"}}}
	r := newRuntimeForTest(Config{Enabled: false}, sessions, fakeChat("nova memória", 10, nil))

	if err := r.RunColdPathOnce(context.Background(), workspace); err != nil {
		t.Fatalf("RunColdPathOnce() error = %v", err)
	}
	if sessions.calls != 0 {
		t.Fatal("expected the disabled runtime not to collect any digests (BR-005)")
	}
	if _, err := os.Stat(filepath.Join(workspace, stateDirRelPath)); !os.IsNotExist(err) {
		t.Fatal("expected no report to be written when disabled")
	}
}

func TestRuntime_DryRun_WritesReportButNotMemory(t *testing.T) {
	workspace := t.TempDir()
	sessions := &fakeSessionSource{digests: []SessionDigest{{SessionID: "s1", Summary: "x"}}}
	r := newRuntimeForTest(Config{Enabled: true, DryRun: true}, sessions, fakeChat("memória nova", 50, nil))

	if err := r.RunColdPathOnce(context.Background(), workspace); err != nil {
		t.Fatalf("RunColdPathOnce() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspace, memoryRelPath)); !os.IsNotExist(err) {
		t.Fatal("dry_run must not write MEMORY.md (AC-010-2)")
	}

	entries, err := os.ReadDir(filepath.Join(workspace, stateDirRelPath))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one report file, err=%v entries=%v", err, entries)
	}
}

func TestRuntime_NotDryRun_WritesMemoryAndReport(t *testing.T) {
	workspace := t.TempDir()
	sessions := &fakeSessionSource{digests: []SessionDigest{{SessionID: "s1", Summary: "x"}}}
	r := newRuntimeForTest(Config{Enabled: true}, sessions, fakeChat("memória consolidada", 50, nil))

	if err := r.RunColdPathOnce(context.Background(), workspace); err != nil {
		t.Fatalf("RunColdPathOnce() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workspace, memoryRelPath))
	if err != nil {
		t.Fatalf("reading MEMORY.md: %v", err)
	}
	if string(data) != "memória consolidada" {
		t.Fatalf("MEMORY.md = %q", string(data))
	}
}

func TestRuntime_SecondRun_UsesPreviousRunAsSince(t *testing.T) {
	workspace := t.TempDir()
	sessions := &fakeSessionSource{digests: []SessionDigest{{SessionID: "s1", Summary: "x"}}}
	r := newRuntimeForTest(Config{Enabled: true}, sessions, fakeChat("memória", 10, nil))
	fixedNow := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return fixedNow }

	if err := r.RunColdPathOnce(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if !sessions.lastSince.IsZero() {
		t.Fatalf("first run: lastSince = %v, want zero", sessions.lastSince)
	}

	if err := r.RunColdPathOnce(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if !sessions.lastSince.Equal(fixedNow) {
		t.Fatalf("second run: lastSince = %v, want %v (the first run's timestamp)", sessions.lastSince, fixedNow)
	}
}

func TestRuntime_WeeklyDeep_ResetsSinceToZero(t *testing.T) {
	workspace := t.TempDir()
	sessions := &fakeSessionSource{digests: []SessionDigest{{SessionID: "s1", Summary: "x"}}}
	r := newRuntimeForTest(Config{Enabled: true, WeeklyDeep: true}, sessions, fakeChat("memória", 10, nil))

	sunday := time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC)
	for sunday.Weekday() != time.Sunday {
		sunday = sunday.AddDate(0, 0, 1)
	}
	r.now = func() time.Time { return sunday }

	// Seed a previous run so a non-deep call would otherwise use a non-zero since.
	r.lastRun[workspace] = sunday.AddDate(0, 0, -1)

	if err := r.RunColdPathOnce(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if !sessions.lastSince.IsZero() {
		t.Fatalf("lastSince = %v, want zero on a weekly-deep run", sessions.lastSince)
	}
	if !sessions.lastDeep {
		t.Fatal("expected weeklyDeep = true to be passed to RecentDigests")
	}
}

func TestRuntime_CollectionError_PropagatesAndSkipsWrite(t *testing.T) {
	workspace := t.TempDir()
	sessions := &fakeSessionSource{err: os.ErrPermission}
	r := newRuntimeForTest(Config{Enabled: true}, sessions, fakeChat("memória", 10, nil))

	if err := r.RunColdPathOnce(context.Background(), workspace); err == nil {
		t.Fatal("expected an error when digest collection fails")
	}
	if _, err := os.Stat(filepath.Join(workspace, stateDirRelPath)); !os.IsNotExist(err) {
		t.Fatal("no report should be written when collection fails before any triage ran")
	}
}
