package cron

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runHistory(t *testing.T, store, id string) string {
	t.Helper()
	cmd := newHistoryCommand(func() string { return store })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{id})
	require.NoError(t, cmd.Execute())
	return out.String()
}

func TestHistoryCommand_ShowsRunsWrittenByGateway(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	job := addEveryJob(t, store)

	assert.Contains(t, runHistory(t, store, job.ID), "No runs recorded")

	started := time.Date(2026, 10, 11, 9, 0, 0, 0, time.Local)
	runs := fmt.Sprintf(`{"version":1,"jobs":{%q:[`+
		`{"startedAtMs":%d,"durationMs":1500,"status":"error","error":"command failed: exit 3"},`+
		`{"startedAtMs":%d,"durationMs":20,"status":"ok"}]}}`,
		job.ID, started.UnixMilli(), started.Add(-time.Hour).UnixMilli())
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(store), "runs.json"), []byte(runs), 0o600))

	out := runHistory(t, store, job.ID)
	assert.Contains(t, out, "2026-10-11 09:00:00, 1500ms, error: command failed: exit 3")
	assert.Contains(t, out, "2026-10-11 08:00:00, 20ms, ok")
}

func TestHistoryCommand_UnknownJobFails(t *testing.T) {
	cmd := newHistoryCommand(func() string { return filepath.Join(t.TempDir(), "jobs.json") })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"missing"})
	require.Error(t, cmd.Execute())
}
