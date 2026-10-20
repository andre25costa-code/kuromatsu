package cron

// Run history.
//
// The last cronRunHistoryPerJob runs of each job are kept in runs.json, next
// to jobs.json. Only the process that executes jobs (the gateway's run loop,
// through executeJobByID) writes this file; the CLI only reads it, through
// History. Every write drops entries of jobs that no longer exist, so a
// one-shot 'at' job deleted after its run loses its history in that same
// write, and History returns nothing for it.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

const (
	cronRunHistoryPerJob = 20
	cronHistoryErrMax    = 512
	runHistoryFileName   = "runs.json"
)

// CronRun is one execution of a job.
type CronRun struct {
	StartedAtMS int64  `json:"startedAtMs"`
	DurationMS  int64  `json:"durationMs"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

type runHistoryFile struct {
	Version int                  `json:"version"`
	Jobs    map[string][]CronRun `json:"jobs"`
}

func (cs *CronService) historyPath() string {
	return filepath.Join(filepath.Dir(cs.storePath), runHistoryFileName)
}

func readRunHistory(path string) (*runHistoryFile, error) {
	history := &runHistoryFile{Version: 1, Jobs: map[string][]CronRun{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return history, nil
		}
		return history, err
	}
	if err := json.Unmarshal(data, history); err != nil {
		return &runHistoryFile{Version: 1, Jobs: map[string][]CronRun{}}, err
	}
	if history.Jobs == nil {
		history.Jobs = map[string][]CronRun{}
	}
	return history, nil
}

// recordRunUnsafe adds run as the newest entry of jobID, keeps at most
// cronRunHistoryPerJob entries per job and drops jobs no longer in the
// store. A failure is logged and never affects the job. Caller holds cs.mu.
func (cs *CronService) recordRunUnsafe(jobID string, run CronRun) {
	path := cs.historyPath()
	history, err := readRunHistory(path)
	if err != nil {
		logger.WarnCF("cron", "Unreadable run history; starting a new one", map[string]any{
			"path": path, "error": err.Error(),
		})
	}

	run.Error = truncateRunError(run.Error)
	runs := append([]CronRun{run}, history.Jobs[jobID]...)
	if len(runs) > cronRunHistoryPerJob {
		runs = runs[:cronRunHistoryPerJob]
	}
	history.Jobs[jobID] = runs

	existing := make(map[string]bool, len(cs.store.Jobs))
	for _, job := range cs.store.Jobs {
		existing[job.ID] = true
	}
	for id := range history.Jobs {
		if !existing[id] {
			delete(history.Jobs, id)
		}
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err == nil {
		err = fileutil.WriteFileAtomic(path, data, 0o600)
	}
	if err != nil {
		logger.WarnCF("cron", "Failed to write run history", map[string]any{
			"path": path, "job_id": jobID, "error": err.Error(),
		})
	}
}

// History returns a copy of the job's recorded runs, most recent first.
func (cs *CronService) History(jobID string) []CronRun {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	path := cs.historyPath()
	history, err := readRunHistory(path)
	if err != nil {
		logger.WarnCF("cron", "Failed to read run history", map[string]any{
			"path": path, "error": err.Error(),
		})
		return nil
	}
	runs := history.Jobs[jobID]
	out := make([]CronRun, len(runs))
	copy(out, runs)
	return out
}

// FormatHistory renders a job's runs, most recent first, with local start
// time, duration, status and error.
func FormatHistory(job *CronJob, runs []CronRun) string {
	if len(runs) == 0 {
		return fmt.Sprintf("No runs recorded for cron job '%s' (%s)", job.Name, job.ID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Runs of cron job '%s' (%s), most recent first:\n", job.Name, job.ID)
	for _, run := range runs {
		fmt.Fprintf(&b, "- %s, %dms, %s",
			time.UnixMilli(run.StartedAtMS).Format("2006-01-02 15:04:05"), run.DurationMS, run.Status)
		if run.Error != "" {
			fmt.Fprintf(&b, ": %s", run.Error)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// truncateRunError caps an error message at cronHistoryErrMax bytes without
// splitting a UTF-8 sequence.
func truncateRunError(msg string) string {
	msg = strings.ToValidUTF8(msg, "�")
	if len(msg) <= cronHistoryErrMax {
		return msg
	}
	cut := cronHistoryErrMax
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut]
}
