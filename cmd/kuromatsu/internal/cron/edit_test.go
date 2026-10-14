package cron

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func runEdit(t *testing.T, store string, args ...string) error {
	t.Helper()
	cmd := newEditCommand(func() string { return store })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestEditCommand_UpdatesCronAndTZ(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	job := addEveryJob(t, store)

	require.NoError(t, runEdit(t, store, job.ID, "--cron", "0 7 * * *", "--tz", "America/Sao_Paulo"))

	got, ok := cron.NewCronService(store, nil).GetJob(job.ID)
	require.True(t, ok)
	assert.Equal(t, cron.CronSchedule{Kind: "cron", Expr: "0 7 * * *", TZ: "America/Sao_Paulo"}, got.Schedule)
	assert.Equal(t, "send the report", got.Payload.Message, "edit kept the untouched fields")
	require.NotNil(t, got.State.NextRunAtMS)
}

func TestEditCommand_InvalidCronFailsWithoutChange(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	job := addEveryJob(t, store)

	require.Error(t, runEdit(t, store, job.ID, "--cron", "invalid"))

	got, ok := cron.NewCronService(store, nil).GetJob(job.ID)
	require.True(t, ok)
	assert.Equal(t, "every", got.Schedule.Kind)
	require.NotNil(t, got.Schedule.EveryMS)
	assert.Equal(t, int64(600_000), *got.Schedule.EveryMS)
}

func TestEditCommand_MessageOnlyAndTZWithoutCron(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	job := addEveryJob(t, store)

	require.NoError(t, runEdit(t, store, job.ID, "--message", "new text", "--name", "renamed"))
	got, _ := cron.NewCronService(store, nil).GetJob(job.ID)
	assert.Equal(t, "new text", got.Payload.Message)
	assert.Equal(t, "renamed", got.Name)

	require.Error(t, runEdit(t, store, job.ID, "--tz", "UTC"))
	require.Error(t, runEdit(t, store, job.ID), "an edit with no field must fail")
}
