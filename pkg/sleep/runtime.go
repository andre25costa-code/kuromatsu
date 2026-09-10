package sleep

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

// memoryRelPath and stateDirRelPath are workspace-relative, matching the
// tracked workspace template (workspace/memory/MEMORY.md) and the
// existing per-workspace state directory convention.
const (
	memoryRelPath   = "memory/MEMORY.md"
	stateDirRelPath = "state"
)

// Runtime ties the triage pipeline to a per-workspace "last run" clock and
// implements the RunColdPathOnce(ctx, workspace string) error method that
// evolution.NewColdPathRunner expects (ADR-006). Passing a *Runtime value
// to that constructor works without pkg/evolution exporting its interface
// type: Go satisfies unexported interface parameters structurally.
type Runtime struct {
	cfg      Config
	sessions SessionSource
	chat     ChatFunc
	now      func() time.Time

	mu      sync.Mutex
	lastRun map[string]time.Time
}

func NewRuntime(cfg Config, sessions SessionSource, chat ChatFunc) *Runtime {
	return &Runtime{
		cfg:      cfg.WithDefaults(),
		sessions: sessions,
		chat:     chat,
		now:      time.Now,
		lastRun:  make(map[string]time.Time),
	}
}

// RunColdPathOnce runs one triage pass for workspace: collects digests
// since the last run (or full history on a weekly-deep day), consolidates
// them into MEMORY.md, and always writes a report under
// <workspace>/state/. AC-010-2/3/5/6.
func (r *Runtime) RunColdPathOnce(ctx context.Context, workspace string) error {
	if !r.cfg.Enabled {
		return nil // BR-005: defense in depth even if a caller triggers a disabled Runtime
	}

	now := r.now()
	weeklyDeep := r.cfg.WeeklyDeep && IsWeeklyDeepDay(now)

	r.mu.Lock()
	since := r.lastRun[workspace]
	r.mu.Unlock()
	if weeklyDeep {
		since = time.Time{}
	}

	digests, err := r.sessions.RecentDigests(ctx, workspace, since, weeklyDeep)
	if err != nil {
		return fmt.Errorf("sleep: collecting session digests: %w", err)
	}

	currentMemory, err := readMemory(workspace)
	if err != nil {
		return fmt.Errorf("sleep: reading MEMORY.md: %w", err)
	}

	updatedMemory, report := runTriage(ctx, r.cfg, weeklyDeep, currentMemory, digests, r.chat)
	report.Date = now

	if report.MemoryUpdated && !r.cfg.DryRun {
		if err := writeMemory(workspace, updatedMemory); err != nil {
			return fmt.Errorf("sleep: writing MEMORY.md: %w", err)
		}
	}

	if err := writeReport(workspace, report); err != nil {
		return fmt.Errorf("sleep: writing report: %w", err)
	}

	r.mu.Lock()
	r.lastRun[workspace] = now
	r.mu.Unlock()

	return nil
}

func readMemory(workspace string) (string, error) {
	data, err := os.ReadFile(filepath.Join(workspace, memoryRelPath))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeMemory(workspace, content string) error {
	return fileutil.WriteFileAtomic(filepath.Join(workspace, memoryRelPath), []byte(content), 0o644)
}

func writeReport(workspace string, report Report) error {
	path := filepath.Join(workspace, stateDirRelPath, ReportFileName(report.Date))
	return fileutil.WriteFileAtomic(path, []byte(report.Render()), 0o644)
}
