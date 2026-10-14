package cron

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestHistory_CapsPerJob(t *testing.T) {
	var result error
	cs, clock, id := scriptedService(t, &result)
	t0 := clock.Now()
	for i := range 25 {
		clock.Set(t0.Add(time.Duration(i) * time.Minute))
		cs.executeJobByID(id)
	}

	runs := cs.History(id)
	if len(runs) != cronRunHistoryPerJob {
		t.Fatalf("history has %d runs, want %d", len(runs), cronRunHistoryPerJob)
	}
	if want := t0.Add(24 * time.Minute).UnixMilli(); runs[0].StartedAtMS != want {
		t.Fatalf("newest run started at %d, want %d", runs[0].StartedAtMS, want)
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].StartedAtMS >= runs[i-1].StartedAtMS {
			t.Fatalf("history not newest-first at %d: %+v", i, runs)
		}
	}
	if runs[0].Status != "ok" {
		t.Fatalf("status = %q, want ok", runs[0].Status)
	}

	runs[0].Status = "mutated"
	if again := cs.History(id); again[0].Status != "ok" {
		t.Fatal("History returned the stored slice, not a copy")
	}
}

func TestHistory_TruncatesOnRuneBoundary(t *testing.T) {
	// 2000 bytes; every 'ç' is 2 bytes and starts at an odd offset, so byte
	// 512 falls inside one.
	msg := "a" + strings.Repeat("ç", 999) + "b"
	if len(msg) != 2000 {
		t.Fatalf("test message is %d bytes", len(msg))
	}
	result := errors.New(msg)
	cs, _, id := scriptedService(t, &result)

	cs.executeJobByID(id)

	runs := cs.History(id)
	if len(runs) != 1 {
		t.Fatalf("history has %d runs, want 1", len(runs))
	}
	got := runs[0].Error
	if got == "" || len(got) > cronHistoryErrMax || !utf8.ValidString(got) {
		t.Fatalf("error kept %d bytes (valid UTF-8 %v), want 1..%d valid bytes",
			len(got), utf8.ValidString(got), cronHistoryErrMax)
	}
	if runs[0].Status != "error" {
		t.Fatalf("status = %q, want error", runs[0].Status)
	}
}

func TestHistory_PrunesRemovedJobs(t *testing.T) {
	var result error
	cs, _, kept := scriptedService(t, &result)
	removed, err := cs.AddJob("other", CronSchedule{Kind: "every", EveryMS: int64Ptr(120_000)}, "x", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}
	cs.executeJobByID(removed.ID)
	if n := len(cs.History(removed.ID)); n != 1 {
		t.Fatalf("history of %s has %d runs, want 1", removed.ID, n)
	}

	cs.RemoveJob(removed.ID)
	cs.executeJobByID(kept)

	if n := len(cs.History(removed.ID)); n != 0 {
		t.Fatalf("history of removed job has %d runs, want 0", n)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cs.storePath), "runs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file runHistoryFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if _, ok := file.Jobs[removed.ID]; ok || file.Version != 1 || len(file.Jobs[kept]) != 1 {
		t.Fatalf("runs.json = %s", data)
	}
}

// A one-shot job deleted after its run leaves no history behind.
func TestHistory_DeletedAtJobHasNoHistory(t *testing.T) {
	cs := NewCronService(
		filepath.Join(t.TempDir(), "jobs.json"),
		func(*CronJob) (string, error) { return "ok", nil },
	)
	at := time.Now().Add(time.Hour).UnixMilli()
	job, err := cs.AddJob("once", CronSchedule{Kind: "at", AtMS: &at}, "msg", "cli", "direct")
	if err != nil {
		t.Fatal(err)
	}
	cs.executeJobByID(job.ID)
	if _, ok := cs.GetJob(job.ID); ok {
		t.Fatal("one-shot job not deleted after its run")
	}
	if n := len(cs.History(job.ID)); n != 0 {
		t.Fatalf("history of deleted one-shot job has %d runs, want 0", n)
	}
}
