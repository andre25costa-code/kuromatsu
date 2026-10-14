package cron

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func TestDisableSubcommand(t *testing.T) {
	fn := func() string { return "" }
	cmd := newDisableCommand(fn)

	require.NotNil(t, cmd)

	assert.Equal(t, "disable", cmd.Use)
	assert.Equal(t, "Disable a job", cmd.Short)

	assert.True(t, cmd.HasExample())
}

// addEveryJob creates a recurring job in store and returns it.
func addEveryJob(t *testing.T, store string) *cron.CronJob {
	t.Helper()
	everyMS := int64(600_000)
	job, err := cron.NewCronService(store, nil).
		AddJob("report", cron.CronSchedule{Kind: "every", EveryMS: &everyMS}, "send the report", "cli", "direct")
	require.NoError(t, err)
	return job
}

func TestDisableCommand_PrintsDisabled(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	job := addEveryJob(t, store)

	cmd := newDisableCommand(func() string { return store })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{job.ID})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "disabled")
	assert.NotContains(t, out.String(), "enabled")
	got, ok := cron.NewCronService(store, nil).GetJob(job.ID)
	require.True(t, ok)
	assert.False(t, got.Enabled)
}

func TestDisableCommand_UnknownJobFails(t *testing.T) {
	cmd := newDisableCommand(func() string { return filepath.Join(t.TempDir(), "jobs.json") })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"missing"})
	require.Error(t, cmd.Execute())
}
