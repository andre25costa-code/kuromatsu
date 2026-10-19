package cron

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // TestComputeNextRun_AppliesTZ needs zone data on any host

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

func TestSaveStore_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")

	cs := NewCronService(storePath, nil)

	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("cron store has permission %04o, want 0600", perm)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func setupService(t *testing.T, handler JobHandler) (*CronService, string) {
	t.Helper()
	// A temp dir, not the package dir: run history is written next to the store.
	tmpFile := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(tmpFile, handler)
	return cs, tmpFile
}

func TestCronService_CRUD(t *testing.T) {
	cs, path := setupService(t, nil)
	defer os.Remove(path)

	// Test AddJob
	at := time.Now().Add(time.Hour).UnixMilli()
	job, err := cs.AddJob("Task1", CronSchedule{Kind: "at", AtMS: &at}, "msg", "ch", "to")
	if err != nil || job.ID == "" {
		t.Fatalf("AddJob failed: %v", err)
	}

	// Test ListJobs
	if len(cs.ListJobs(true)) != 1 {
		t.Error("ListJobs should return 1 job")
	}

	// Test UpdateJob
	job.Name = "UpdatedName"
	err = cs.UpdateJob(job)
	if err != nil || cs.store.Jobs[0].Name != "UpdatedName" {
		t.Error("UpdateJob failed")
	}

	// Test EnableJob
	if _, err = cs.EnableJob(job.ID, false); err != nil {
		t.Fatalf("EnableJob(false): %v", err)
	}
	if cs.store.Jobs[0].Enabled != false || cs.store.Jobs[0].State.NextRunAtMS != nil {
		t.Error("EnableJob(false) failed to clear state")
	}

	// Test RemoveJob
	removed, err := cs.RemoveJob(job.ID)
	if err != nil || !removed || len(cs.store.Jobs) != 0 {
		t.Error("RemoveJob failed")
	}
}

func TestCronService_GetJobReturnsCopy(t *testing.T) {
	cs, path := setupService(t, nil)
	defer os.Remove(path)

	everyMS := int64(60_000)
	job, err := cs.AddJob("Task1", CronSchedule{Kind: "every", EveryMS: &everyMS}, "msg", "ch", "to")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}
	if job.State.NextRunAtMS == nil {
		t.Fatal("expected initial next run")
	}
	nextRun := *job.State.NextRunAtMS

	got, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("GetJob should find job")
	}
	got.Name = "mutated"
	got.Payload.Message = "changed"
	if got.Schedule.EveryMS != nil {
		*got.Schedule.EveryMS = 120_000
	}
	if got.State.NextRunAtMS != nil {
		*got.State.NextRunAtMS = time.Now().Add(3 * time.Hour).UnixMilli()
	}

	again, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("GetJob should still find job")
	}
	if again.Name != "Task1" || again.Payload.Message != "msg" {
		t.Fatalf("GetJob should return a copy, got %+v", again)
	}
	if again.Schedule.EveryMS == nil || *again.Schedule.EveryMS != everyMS {
		t.Fatalf("GetJob should not alias schedule pointers, got %+v", again.Schedule)
	}
	if again.State.NextRunAtMS == nil || *again.State.NextRunAtMS != nextRun {
		t.Fatalf("GetJob should not alias state pointers, got %+v", again.State)
	}
}

func TestCronService_UpdateJobRecomputesNextRunOnScheduleOrEnabledChange(t *testing.T) {
	cs, path := setupService(t, nil)
	defer os.Remove(path)

	at := time.Now().Add(time.Hour).UnixMilli()
	job, err := cs.AddJob("Task1", CronSchedule{Kind: "at", AtMS: &at}, "msg", "ch", "to")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}
	if job.State.NextRunAtMS == nil {
		t.Fatal("expected initial next run")
	}
	initialNextRun := *job.State.NextRunAtMS

	everyMS := int64(30_000)
	job.Schedule = CronSchedule{Kind: "every", EveryMS: &everyMS}
	if err := cs.UpdateJob(job); err != nil {
		t.Fatalf("UpdateJob schedule failed: %v", err)
	}
	updated, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("updated job not found")
	}
	if updated.State.NextRunAtMS == nil {
		t.Fatal("expected recomputed next run after schedule change")
	}
	if *updated.State.NextRunAtMS == initialNextRun {
		t.Fatalf("next run should be recomputed, still %d", initialNextRun)
	}

	if disabled, err := cs.EnableJob(job.ID, false); err != nil || disabled == nil {
		t.Fatalf("EnableJob(false) = %v, %v", disabled, err)
	}
	disabled, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("disabled job not found")
	}
	disabled.Enabled = true
	if err := cs.UpdateJob(disabled); err != nil {
		t.Fatalf("UpdateJob enabled failed: %v", err)
	}
	reenabled, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("reenabled job not found")
	}
	if !reenabled.Enabled || reenabled.State.NextRunAtMS == nil {
		t.Fatalf("expected enabled job with next run, got %+v", reenabled)
	}
}

func TestCronService_UpdateJobPreservesRunStateOnPayloadOnlyChange(t *testing.T) {
	cs, path := setupService(t, nil)
	defer os.Remove(path)

	everyMS := int64(60_000)
	job, err := cs.AddJob("Task1", CronSchedule{Kind: "every", EveryMS: &everyMS}, "msg", "ch", "to")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}
	lastRun := time.Now().Add(-time.Minute).UnixMilli()
	job.State.LastRunAtMS = &lastRun
	job.State.LastStatus = "ok"
	job.State.LastError = "previous"
	if job.State.NextRunAtMS == nil {
		t.Fatal("expected next run before update")
	}
	nextRun := *job.State.NextRunAtMS

	job.Payload.Message = "updated msg"
	if err := cs.UpdateJob(job); err != nil {
		t.Fatalf("UpdateJob failed: %v", err)
	}

	updated, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("updated job not found")
	}
	if updated.State.LastRunAtMS == nil || *updated.State.LastRunAtMS != lastRun {
		t.Fatalf("last run changed: %+v", updated.State)
	}
	if updated.State.LastStatus != "ok" || updated.State.LastError != "previous" {
		t.Fatalf("last status changed: %+v", updated.State)
	}
	if updated.State.NextRunAtMS == nil || *updated.State.NextRunAtMS != nextRun {
		t.Fatalf("next run should be preserved: before=%d after=%+v", nextRun, updated.State.NextRunAtMS)
	}
}

// 2. Test Cron Expression Calculation Logic
func TestCronService_ComputeNextRun(t *testing.T) {
	cs, path := setupService(t, nil)
	defer os.Remove(path)

	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).UnixMilli()

	tests := []struct {
		name     string
		schedule CronSchedule
		wantNil  bool
	}{
		{"Valid Cron", CronSchedule{Kind: "cron", Expr: "0 * * * *"}, false},
		{"Invalid Cron", CronSchedule{Kind: "cron", Expr: "invalid"}, true},
		{"Every MS", CronSchedule{Kind: "every", EveryMS: int64Ptr(5000)}, false},
		{"At Future", CronSchedule{Kind: "at", AtMS: int64Ptr(now + 1000)}, false},
		{"At Past", CronSchedule{Kind: "at", AtMS: int64Ptr(now - 1000)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cs.computeNextRun(&tt.schedule, now)
			if (got == nil) != tt.wantNil {
				t.Errorf("%s: got %v, wantNil %v", tt.name, got, tt.wantNil)
			}
		})
	}
}

// 3. Test Execution Flow
func TestCronService_ExecutionFlow(t *testing.T) {
	var mu sync.Mutex
	executedJobs := make(map[string]bool)

	handler := func(job *CronJob) (string, error) {
		mu.Lock()
		executedJobs[job.ID] = true
		mu.Unlock()
		return "ok", nil
	}

	cs, path := setupService(t, handler)
	defer os.Remove(path)

	// Start the service
	if err := cs.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer cs.Stop()

	// Add a job then runs 100ms from now
	target := time.Now().Add(100 * time.Millisecond).UnixMilli()
	job, _ := cs.AddJob("FastJob", CronSchedule{Kind: "at", AtMS: &target}, "", "", "")

	// Check for job execution with a timeout
	success := false
	for range 20 {
		mu.Lock()
		if executedJobs[job.ID] {
			success = true
			mu.Unlock()
			break
		}
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}

	if !success {
		t.Error("Job was not executed in time")
	}

	// check that the job is removed after execution (DeleteAfterRun = true)
	status := cs.Status()
	if status["jobs"].(int) != 0 {
		t.Errorf("Job should be deleted after run, got count: %v", status["jobs"])
	}
}

func TestCronService_PersistenceIntegrity(t *testing.T) {
	tmpFile := "persist_test.json"
	defer os.Remove(tmpFile)

	// write a job and persist
	cs1 := NewCronService(tmpFile, nil)
	at := int64(2000000000000)
	cs1.AddJob("PersistMe", CronSchedule{Kind: "at", AtMS: &at}, "payload", "ch1", "")

	// check file exists
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Fatal("Store file was not created")
	}

	// reload and check data integrity
	cs2 := NewCronService(tmpFile, nil)
	if err := cs2.Load(); err != nil {
		t.Fatalf("Failed to load store: %v", err)
	}

	jobs := cs2.ListJobs(true)
	if len(jobs) != 1 || jobs[0].Name != "PersistMe" {
		t.Errorf("Data corruption after reload. Got: %+v", jobs)
	}

	// test loading invalid JSON
	os.WriteFile(tmpFile, []byte("{invalid json}"), 0o644)
	cs3 := NewCronService(tmpFile, nil)
	err := cs3.loadStore()
	if err == nil {
		t.Error("Should return error when loading invalid JSON")
	}
}

func TestCronService_ConcurrentAccess(t *testing.T) {
	cs, path := setupService(t, nil)
	defer os.Remove(path)

	cs.Start()
	defer cs.Stop()

	var wg sync.WaitGroup
	workers := 10
	iterations := 50

	wg.Add(workers * 2)

	// add jobs concurrently
	for i := range workers {
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				at := time.Now().Add(time.Hour).UnixMilli()
				cs.AddJob(fmt.Sprintf("Job-%d-%d", id, j), CronSchedule{Kind: "at", AtMS: &at}, "", "", "")
				time.Sleep(100 * time.Microsecond)
			}
		}(i)
	}

	// read and update jobs concurrently
	for range workers {
		go func() {
			defer wg.Done()
			for j := range iterations {
				jobs := cs.ListJobs(true)
				if len(jobs) > 0 {
					_, _ = cs.EnableJob(jobs[0].ID, j%2 == 0)
				}
				time.Sleep(100 * time.Microsecond)
			}
		}()
	}

	wg.Wait()
}

// AC-028-6: the 2026-09-21 incident -- the model repeated the same
// "cron add" every heartbeat and each call created a new job (7 duplicates).
func TestAddJob_IdenticalJobIsNotDuplicated(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	schedule := CronSchedule{Kind: "cron", Expr: "0 6 * * *"}

	first, err := cs.AddJob("check", schedule, "check the schedule", "telegram", "123")
	if err != nil {
		t.Fatal(err)
	}
	second, err := cs.AddJob("check", schedule, "check the schedule", "telegram", "123")
	if err != nil {
		t.Fatal(err)
	}

	if second.ID != first.ID {
		t.Fatalf("second AddJob created %s, want the existing %s", second.ID, first.ID)
	}
	if n := len(cs.ListJobs(true)); n != 1 {
		t.Fatalf("ListJobs() has %d jobs, want 1", n)
	}
}

func TestAddJob_DifferentDestinationOrScheduleIsNewJob(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	daily := CronSchedule{Kind: "cron", Expr: "0 6 * * *"}

	if _, err := cs.AddJob("check", daily, "msg", "telegram", "123"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AddJob("check", daily, "msg", "telegram", "456"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AddJob(
		"check",
		CronSchedule{Kind: "cron", Expr: "0 7 * * *"},
		"msg",
		"telegram",
		"123",
	); err != nil {
		t.Fatal(err)
	}
	if n := len(cs.ListJobs(true)); n != 3 {
		t.Fatalf("ListJobs() has %d jobs, want 3", n)
	}
}

// T15: notify must not drop the wake-up when the run loop is busy outside its
// select (e.g. between getNextWakeMS and the select): with an unbuffered
// channel the default branch discarded it, and a job added in that window
// waited for the current timer (up to an hour with no jobs).
func TestNotify_KeepsWakeUpWhenLoopIsNotWaiting(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	cs.notify() // nobody is receiving right now
	select {
	case <-cs.wakeChan:
	default:
		t.Fatal("wake-up dropped: the loop would sleep on a stale timer")
	}
	cs.notify()
	cs.notify() // a second pending signal adds nothing and must not block
}

// T16: a command job is created with its whole payload in one locked, single
// save -- never persisted first as a plain message job and patched afterwards.
func TestAddJobWithPayload_PersistsFullPayloadAtOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(path, nil)
	payload := CronPayload{
		Message:      "carga",
		Command:      "uptime",
		Quiet:        true,
		AnomalyRegex: "load: [5-9]",
		Window:       "cron",
	}

	job, created, err := cs.AddJobWithPayload("carga", CronSchedule{Kind: "cron", Expr: "0 7 * * *"}, payload)
	if err != nil || !created {
		t.Fatalf("AddJobWithPayload = created %v, err %v", created, err)
	}

	reloaded := NewCronService(path, nil)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.GetJob(job.ID)
	if !ok {
		t.Fatal("job not persisted")
	}
	p := got.Payload
	if p.Command != "uptime" || !p.Quiet || p.AnomalyRegex != "load: [5-9]" || p.Window != "cron" ||
		p.Kind != "agent_turn" {
		t.Fatalf("persisted payload = %+v", p)
	}
}

func TestAddJobWithPayload_IdenticalJobIsNotDuplicated(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	sched := CronSchedule{Kind: "cron", Expr: "0 7 * * *"}
	payload := CronPayload{Message: "carga", Command: "uptime"}
	first, _, err := cs.AddJobWithPayload("carga", sched, payload)
	if err != nil {
		t.Fatal(err)
	}
	again, created, err := cs.AddJobWithPayload("carga", sched, payload)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second add: id %s (first %s), created %v, err %v", again.ID, first.ID, created, err)
	}
	if n := len(cs.ListJobs(true)); n != 1 {
		t.Fatalf("jobs = %d, want 1", n)
	}
}

// Same name/schedule/message/destination but a different command is an
// explicit error, and the existing job is left untouched.
func TestAddJobWithPayload_DifferentCommandIsRejected(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	sched := CronSchedule{Kind: "cron", Expr: "0 7 * * *"}
	first, _, err := cs.AddJobWithPayload("carga", sched, CronPayload{Message: "carga", Command: "uptime"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = cs.AddJobWithPayload("carga", sched, CronPayload{Message: "carga", Command: "free -m"})
	if !errors.Is(err, ErrJobExistsWithDifferentCommand) {
		t.Fatalf("err = %v, want ErrJobExistsWithDifferentCommand", err)
	}
	kept, _ := cs.GetJob(first.ID)
	if kept.Payload.Command != "uptime" {
		t.Fatalf("existing job modified: command %q", kept.Payload.Command)
	}
}

// --- Reliable status, auto-disable, validation, time zone, reload ---

// fakeClock is a settable clock for the service's run timing.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// scriptedService returns a service whose handler returns *result, with an
// injected clock and one recurring job.
func scriptedService(t *testing.T, result *error) (*CronService, *fakeClock, string) {
	t.Helper()
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), func(*CronJob) (string, error) {
		if *result != nil {
			return "", *result
		}
		return "ok", nil
	})
	clock := &fakeClock{t: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)}
	cs.now = clock.Now
	job, err := cs.AddJob("job", CronSchedule{Kind: "every", EveryMS: int64Ptr(60_000)}, "msg", "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	return cs, clock, job.ID
}

func mustGetJob(t *testing.T, cs *CronService, id string) *CronJob {
	t.Helper()
	job, ok := cs.GetJob(id)
	if !ok {
		t.Fatalf("job %s not found", id)
	}
	return job
}

func TestExecuteJob_HandlerErrorSetsErrorStatus(t *testing.T) {
	result := errors.New("provider unreachable")
	cs, _, id := scriptedService(t, &result)

	cs.executeJobByID(id)

	job := mustGetJob(t, cs, id)
	if job.State.LastStatus != "error" || !strings.Contains(job.State.LastError, "provider unreachable") {
		t.Fatalf("state = %+v, want error status with the handler error", job.State)
	}
	if job.State.ConsecutiveFailures != 1 || job.State.FirstFailureAtMS == nil {
		t.Fatalf("failure streak = %d (first %v), want 1 with a start time",
			job.State.ConsecutiveFailures, job.State.FirstFailureAtMS)
	}
}

func TestExecuteJob_CanceledIsNeutral(t *testing.T) {
	result := errors.New("boom")
	cs, clock, id := scriptedService(t, &result)
	t0 := clock.Now()
	cs.executeJobByID(id)

	clock.Set(t0.Add(10 * time.Minute))
	result = fmt.Errorf("cron turn: %w", context.Canceled)
	cs.executeJobByID(id)

	job := mustGetJob(t, cs, id)
	if job.State.LastStatus != "canceled" {
		t.Fatalf("LastStatus = %q, want canceled", job.State.LastStatus)
	}
	if job.State.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1 (unchanged by a canceled run)", job.State.ConsecutiveFailures)
	}
	if job.State.FirstFailureAtMS == nil || *job.State.FirstFailureAtMS != t0.UnixMilli() {
		t.Fatalf("FirstFailureAtMS = %v, want %d", job.State.FirstFailureAtMS, t0.UnixMilli())
	}
}

func TestAutoDisable_RequiresCountAndWindow(t *testing.T) {
	t.Run("count and one hour reached", func(t *testing.T) {
		result := errors.New("boom")
		cs, clock, id := scriptedService(t, &result)
		cs.SetMaxConsecutiveFailures(3)
		var notified []CronJob
		cs.SetOnAutoDisable(func(job CronJob) { notified = append(notified, job) })
		t0 := clock.Now()

		cs.executeJobByID(id)
		clock.Set(t0.Add(30 * time.Minute))
		cs.executeJobByID(id)
		if job := mustGetJob(t, cs, id); !job.Enabled {
			t.Fatalf("disabled after 2 failures: %+v", job.State)
		}

		clock.Set(t0.Add(61 * time.Minute))
		cs.executeJobByID(id)
		job := mustGetJob(t, cs, id)
		if job.Enabled || job.State.NextRunAtMS != nil {
			t.Fatalf("job still enabled after 3 failures over 61 min: enabled=%v next=%v",
				job.Enabled, job.State.NextRunAtMS)
		}
		if !strings.HasPrefix(job.State.LastError, "auto-disabled after 3 consecutive failures: boom") {
			t.Fatalf("LastError = %q", job.State.LastError)
		}
		if len(notified) != 1 || notified[0].ID != id {
			t.Fatalf("onAutoDisable called %d times (%+v), want once for %s", len(notified), notified, id)
		}
	})

	t.Run("one hour not reached", func(t *testing.T) {
		result := errors.New("boom")
		cs, clock, id := scriptedService(t, &result)
		cs.SetMaxConsecutiveFailures(3)
		calls := 0
		cs.SetOnAutoDisable(func(CronJob) { calls++ })
		t0 := clock.Now()
		for _, offset := range []time.Duration{0, 5 * time.Minute, 10 * time.Minute} {
			clock.Set(t0.Add(offset))
			cs.executeJobByID(id)
		}
		job := mustGetJob(t, cs, id)
		if !job.Enabled || job.State.ConsecutiveFailures != 3 || calls != 0 {
			t.Fatalf("enabled=%v failures=%d calls=%d, want still enabled with 3 failures",
				job.Enabled, job.State.ConsecutiveFailures, calls)
		}
	})
}

func TestAutoDisable_SuccessResetsCounter(t *testing.T) {
	result := errors.New("boom")
	cs, _, id := scriptedService(t, &result)

	cs.executeJobByID(id)
	result = nil
	cs.executeJobByID(id)
	if job := mustGetJob(t, cs, id); job.State.ConsecutiveFailures != 0 || job.State.FirstFailureAtMS != nil {
		t.Fatalf("success did not reset the streak: %+v", job.State)
	}
	result = errors.New("boom again")
	cs.executeJobByID(id)

	if job := mustGetJob(t, cs, id); job.State.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1", job.State.ConsecutiveFailures)
	}
}

func TestAutoDisable_ZeroNeverDisables(t *testing.T) {
	result := errors.New("boom")
	cs, clock, id := scriptedService(t, &result)
	cs.SetMaxConsecutiveFailures(0)
	t0 := clock.Now()
	for i := range 10 {
		clock.Set(t0.Add(time.Duration(i) * time.Hour))
		cs.executeJobByID(id)
	}
	if job := mustGetJob(t, cs, id); !job.Enabled || job.State.ConsecutiveFailures != 10 {
		t.Fatalf("enabled=%v failures=%d, want enabled with 10 failures", job.Enabled, job.State.ConsecutiveFailures)
	}
}

func TestEnableJob_ResetsFailureState(t *testing.T) {
	result := errors.New("boom")
	cs, clock, id := scriptedService(t, &result)
	cs.SetMaxConsecutiveFailures(3)
	t0 := clock.Now()
	for _, offset := range []time.Duration{0, 30 * time.Minute, 61 * time.Minute} {
		clock.Set(t0.Add(offset))
		cs.executeJobByID(id)
	}
	if job := mustGetJob(t, cs, id); job.Enabled {
		t.Fatal("precondition: job should be auto-disabled")
	}

	job, err := cs.EnableJob(id, true)
	if err != nil {
		t.Fatalf("EnableJob(true): %v", err)
	}
	if job.State.ConsecutiveFailures != 0 || job.State.FirstFailureAtMS != nil {
		t.Fatalf("streak not reset: %+v", job.State)
	}
	if job.State.LastError != "boom" {
		t.Fatalf("LastError = %q, want the prefix removed", job.State.LastError)
	}

	clock.Set(t0.Add(3 * time.Hour))
	cs.executeJobByID(id)
	job = mustGetJob(t, cs, id)
	if !job.Enabled || job.State.ConsecutiveFailures != 1 {
		t.Fatalf("enabled=%v failures=%d, want enabled with 1 failure", job.Enabled, job.State.ConsecutiveFailures)
	}
}

func TestAddJob_RejectsInvalidSchedules(t *testing.T) {
	past := time.Now().Add(-time.Minute).UnixMilli()
	cases := []struct {
		name     string
		schedule CronSchedule
		wantErr  string
	}{
		{"invalid expression", CronSchedule{Kind: "cron", Expr: "invalid"}, `invalid cron expression "invalid"`},
		{"at in the past", CronSchedule{Kind: "at", AtMS: &past}, "'at' time is in the past"},
		{
			"unknown time zone",
			CronSchedule{Kind: "cron", Expr: "0 9 * * *", TZ: "Mars/Olympus"},
			`invalid time zone "Mars/Olympus"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jobs.json")
			cs := NewCronService(path, nil)
			_, err := cs.AddJob("bad", tc.schedule, "msg", "cli", "direct")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("AddJob err = %v, want %q", err, tc.wantErr)
			}
			if n := len(cs.ListJobs(true)); n != 0 {
				t.Fatalf("store has %d jobs, want 0", n)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("store file written for a rejected job (stat err %v)", statErr)
			}
		})
	}
}

func TestUpdateJob_ValidatesOnlyChangedSchedule(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	job, err := cs.AddJob("daily", CronSchedule{Kind: "cron", Expr: "0 9 * * *"}, "msg", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}
	job.Schedule = CronSchedule{Kind: "cron", Expr: "invalid"}
	if err := cs.UpdateJob(job); err == nil {
		t.Fatal("UpdateJob to an invalid schedule succeeded")
	}
	if kept := mustGetJob(t, cs, job.ID); kept.Schedule.Expr != "0 9 * * *" {
		t.Fatalf("schedule changed to %q", kept.Schedule.Expr)
	}

	// A legacy job already on disk with an expired 'at' stays editable.
	past := time.Now().Add(-time.Hour).UnixMilli()
	cs.mu.Lock()
	cs.store.Jobs = append(cs.store.Jobs, CronJob{
		ID:       "legacy",
		Name:     "legacy",
		Enabled:  true,
		Schedule: CronSchedule{Kind: "at", AtMS: &past},
		Payload:  CronPayload{Kind: "agent_turn", Message: "old", Channel: "cli", To: "direct"},
	})
	saveErr := cs.saveStoreUnsafe()
	cs.mu.Unlock()
	if saveErr != nil {
		t.Fatal(saveErr)
	}

	legacy := mustGetJob(t, cs, "legacy")
	legacy.Payload.Message = "new message"
	if err := cs.UpdateJob(legacy); err != nil {
		t.Fatalf("UpdateJob of a legacy job's message: %v", err)
	}
	if got := mustGetJob(t, cs, "legacy"); got.Payload.Message != "new message" {
		t.Fatalf("message = %q", got.Payload.Message)
	}
	if _, err := cs.EnableJob("legacy", true); err == nil {
		t.Fatal("EnableJob(true) of an expired 'at' job succeeded")
	}
}

func TestComputeNextRun_AppliesTZ(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	ref := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC).UnixMilli()

	cases := []struct {
		tz   string
		want time.Time
	}{
		{"America/Sao_Paulo", time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)},
		{"UTC", time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		got := cs.computeNextRun(&CronSchedule{Kind: "cron", Expr: "0 9 * * *", TZ: tc.tz}, ref)
		if got == nil {
			t.Fatalf("%s: no next run", tc.tz)
		}
		if gotTime := time.UnixMilli(*got).UTC(); !gotTime.Equal(tc.want) {
			t.Fatalf("%s: next run = %s, want %s", tc.tz, gotTime, tc.want)
		}
	}
}

// Two instances on one file: one plays the gateway, the other the CLI.
func TestReload_CLIMutationNotOverwrittenByGateway(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	gateway := NewCronService(path, nil)
	every := CronSchedule{Kind: "every", EveryMS: int64Ptr(600_000)}
	first, err := gateway.AddJob("first", every, "a", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}

	cli := NewCronService(path, nil)
	if _, err := cli.EnableJob(first.ID, false); err != nil {
		t.Fatal(err)
	}

	if _, err := gateway.AddJob("second", every, "b", "cli", "direct"); err != nil {
		t.Fatal(err)
	}

	final := NewCronService(path, nil)
	if jobs := final.ListJobs(true); len(jobs) != 2 {
		t.Fatalf("file has %d jobs, want 2", len(jobs))
	}
	if got := mustGetJob(t, final, first.ID); got.Enabled {
		t.Fatal("the gateway's save overwrote the CLI's disable")
	}
}

func startedGateway(t *testing.T, path string, ran chan<- string) *CronService {
	t.Helper()
	gateway := NewCronService(path, func(job *CronJob) (string, error) {
		select {
		case ran <- job.ID:
		default:
		}
		return "ok", nil
	})
	gateway.maxSleep = 200 * time.Millisecond
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gateway.Stop)
	return gateway
}

func TestReload_KeepsRunNowFromOtherInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	seed := NewCronService(path, nil)
	job, err := seed.AddJob("yearly", CronSchedule{Kind: "cron", Expr: "0 0 1 1 *"}, "msg", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}

	ran := make(chan string, 1)
	startedGateway(t, path, ran)
	// Let the loop compute its wake time (next January) and go to sleep, so
	// the run-now has to arrive through a maxSleep wake-up and reload.
	time.Sleep(50 * time.Millisecond)

	cli := NewCronService(path, nil)
	if err := cli.RunNow(job.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	select {
	case id := <-ran:
		if id != job.ID {
			t.Fatalf("ran %s, want %s", id, job.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the gateway did not run the job marked run-now by another instance")
	}
}

func TestRunLoop_PicksUpJobAddedByOtherInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	ran := make(chan string, 1)
	gateway := startedGateway(t, path, ran)

	cli := NewCronService(path, nil)
	job, err := cli.AddJob("fast", CronSchedule{Kind: "every", EveryMS: int64Ptr(1000)}, "msg", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case id := <-ran:
		if id != job.ID {
			t.Fatalf("ran %s, want %s", id, job.ID)
		}
	case <-time.After(gateway.maxSleep + 2*time.Second):
		t.Fatal("the gateway did not pick up a job added by another instance")
	}
}

func TestRunNow_DisabledJobErrors(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	job, err := cs.AddJob("job", CronSchedule{Kind: "every", EveryMS: int64Ptr(60_000)}, "msg", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cs.EnableJob(job.ID, false); err != nil {
		t.Fatal(err)
	}
	err = cs.RunNow(job.ID)
	if !errors.Is(err, ErrJobDisabled) || err.Error() != "job is disabled; enable it first" {
		t.Fatalf("RunNow on a disabled job = %v, want %q", err, ErrJobDisabled)
	}
}

func TestListJobs_ReturnsCopy(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	job, err := cs.AddJob("job", CronSchedule{Kind: "every", EveryMS: int64Ptr(60_000)}, "msg", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}

	jobs := cs.ListJobs(true)
	jobs[0].Name = "mutated"
	*jobs[0].Schedule.EveryMS = 1
	*jobs[0].State.NextRunAtMS = 0

	got := mustGetJob(t, cs, job.ID)
	if got.Name != "job" || *got.Schedule.EveryMS != 60_000 || *got.State.NextRunAtMS == 0 {
		t.Fatalf("ListJobs aliases the store: %+v", got)
	}
}

// --- Fix round 1 ---

func TestUpdateJob_FailureStreakResetOnlyOnScheduleOrPayloadChange(t *testing.T) {
	result := errors.New("boom")
	cs, _, id := scriptedService(t, &result)

	// Give the job an on_anomaly payload: GetJob clones that pointer, and an
	// unchanged clone must not count as a payload change.
	withAnomaly := mustGetJob(t, cs, id)
	withAnomaly.Payload.OnAnomaly = &CronAnomaly{Message: "explain"}
	if err := cs.UpdateJob(withAnomaly); err != nil {
		t.Fatal(err)
	}
	fail := func() {
		t.Helper()
		cs.executeJobByID(id)
		cs.executeJobByID(id)
		if got := mustGetJob(t, cs, id); got.State.ConsecutiveFailures != 2 || got.State.FirstFailureAtMS == nil {
			t.Fatalf("precondition: streak = %+v", got.State)
		}
	}
	update := func(change func(*CronJob)) CronJobState {
		t.Helper()
		job := mustGetJob(t, cs, id)
		change(job)
		if err := cs.UpdateJob(job); err != nil {
			t.Fatal(err)
		}
		return mustGetJob(t, cs, id).State
	}

	fail()
	st := update(func(j *CronJob) { j.Name = "renamed" })
	if st.ConsecutiveFailures != 2 || st.FirstFailureAtMS == nil {
		t.Fatalf("name-only update reset the streak: %+v", st)
	}

	st = update(func(j *CronJob) { j.Payload.Message = "new" })
	if st.ConsecutiveFailures != 0 || st.FirstFailureAtMS != nil {
		t.Fatalf("payload update kept the streak: %+v", st)
	}

	fail()
	st = update(func(j *CronJob) { j.Schedule = CronSchedule{Kind: "every", EveryMS: int64Ptr(120_000)} })
	if st.ConsecutiveFailures != 0 || st.FirstFailureAtMS != nil {
		t.Fatalf("schedule update kept the streak: %+v", st)
	}
}

// A one-shot job whose run was stopped by shutdown is kept, enabled, with a
// retry time that survives a restart.
func TestExecuteJob_CanceledAtJobIsKeptForRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(path, func(*CronJob) (string, error) {
		return "", fmt.Errorf("cron turn: %w", context.Canceled)
	})
	at := time.Now().Add(time.Hour).UnixMilli()
	job, err := cs.AddJob("once", CronSchedule{Kind: "at", AtMS: &at}, "remind me", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().UnixMilli()
	cs.executeJobByID(job.ID)

	kept, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("a canceled one-shot job was deleted")
	}
	if !kept.Enabled || kept.State.LastStatus != "canceled" || kept.State.NextRunAtMS == nil ||
		*kept.State.NextRunAtMS <= before {
		t.Fatalf("canceled one-shot job = enabled %v, state %+v; want enabled with a retry time",
			kept.Enabled, kept.State)
	}
	retry := *kept.State.NextRunAtMS

	restarted := NewCronService(path, nil)
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	defer restarted.Stop()
	after, ok := restarted.GetJob(job.ID)
	if !ok || after.State.NextRunAtMS == nil || *after.State.NextRunAtMS != retry {
		t.Fatalf("retry time lost on restart: %+v", after)
	}
}

func TestRemoveJob_ReportsSaveFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(path, nil)
	job, err := cs.AddJob("job", CronSchedule{Kind: "every", EveryMS: int64Ptr(60_000)}, "msg", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}
	// A non-empty directory where jobs.json was makes the atomic rename fail.
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(path, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := cs.RemoveJob(job.ID)
	if err == nil {
		t.Fatalf("RemoveJob = %v, nil; want the save error", removed)
	}
}

func TestSetMaxSleep_NonPositiveFallsBackToDefault(t *testing.T) {
	cs := NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	for _, d := range []time.Duration{0, -time.Second} {
		cs.SetMaxSleep(d)
		if cs.maxSleep != defaultMaxSleep {
			t.Fatalf("SetMaxSleep(%v) left maxSleep = %v, want %v", d, cs.maxSleep, defaultMaxSleep)
		}
	}
}

func TestSaveStoreFailure_LogsThroughLogger(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "log.txt")
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatal(err)
	}
	defer logger.DisableFileLogging()

	// The store path sits under a regular file, so every write fails.
	cs := NewCronService(filepath.Join(blocker, "jobs.json"), nil)
	_, err := cs.AddJob("failing-save", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "msg", "cli", "direct")
	if err == nil {
		t.Fatal("AddJob with an unwritable store path returned no error")
	}

	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	logged := string(data)
	if !strings.Contains(logged, `"level":"error"`) || !strings.Contains(logged, `"component":"cron"`) {
		t.Errorf("store write failure not logged as error/cron:\n%s", logged)
	}
}

func TestCheckAgentTurnInterval(t *testing.T) {
	ms := func(d time.Duration) *int64 { v := d.Milliseconds(); return &v }
	for _, tc := range []struct {
		name    string
		s       CronSchedule
		command string
		wantErr bool
	}{
		{"agent turn too fast", CronSchedule{Kind: "every", EveryMS: ms(time.Minute)}, "", true},
		{"agent turn at the minimum", CronSchedule{Kind: "every", EveryMS: ms(MinAgentTurnInterval)}, "", false},
		{"command job may be fast", CronSchedule{Kind: "every", EveryMS: ms(time.Minute)}, "echo hi", false},
		{"blank command is an agent turn", CronSchedule{Kind: "every", EveryMS: ms(time.Minute)}, "  ", true},
		{"cron expression is not checked", CronSchedule{Kind: "cron", Expr: "* * * * *"}, "", false},
	} {
		if err := CheckAgentTurnInterval(tc.s, tc.command); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}
