package cron

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func TestRunNowCommand_MarksJobDue(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	job := addEveryJob(t, store)
	before := time.Now().UnixMilli()

	cmd := newRunNowCommand(func() string { return store })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{job.ID})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "scheduled to run now; the gateway must be running")
	got, ok := cron.NewCronService(store, nil).GetJob(job.ID)
	require.True(t, ok)
	require.NotNil(t, got.State.NextRunAtMS)
	assert.GreaterOrEqual(t, *got.State.NextRunAtMS, before)
	assert.LessOrEqual(t, *got.State.NextRunAtMS, time.Now().UnixMilli())
}

func TestRunNowCommand_DisabledJobFails(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	job := addEveryJob(t, store)
	_, err := cron.NewCronService(store, nil).EnableJob(job.ID, false)
	require.NoError(t, err)

	cmd := newRunNowCommand(func() string { return store })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{job.ID})
	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "job is disabled; enable it first")
}
