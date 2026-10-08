package cron

import (
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func TestNewAddSubcommand(t *testing.T) {
	fn := func() string { return "" }
	cmd := newAddCommand(fn)

	require.NotNil(t, cmd)

	assert.Equal(t, "add", cmd.Use)
	assert.Equal(t, "Add a new scheduled job", cmd.Short)

	assert.True(t, cmd.HasFlags())

	assert.NotNil(t, cmd.Flags().Lookup("every"))
	assert.NotNil(t, cmd.Flags().Lookup("cron"))
	assert.NotNil(t, cmd.Flags().Lookup("to"))
	assert.NotNil(t, cmd.Flags().Lookup("channel"))

	nameFlag := cmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag)

	require.NotNil(t, cmd.Flags().Lookup("message"))
	require.NotNil(t, cmd.Flags().Lookup("command"))

	val, found := nameFlag.Annotations[cobra.BashCompOneRequiredFlag]
	require.True(t, found)
	require.NotEmpty(t, val)
	assert.Equal(t, "true", val[0])
}

// AC-028-7: a deterministic command job (0 tokens) can be created from the
// CLI -- before, only by hand-editing jobs.json (incident of 2026-09-21).
func TestAddCommand_CommandJobPersistsPayloadCommand(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cmd := newAddCommand(func() string { return store })
	cmd.SetArgs([]string{
		"--name", "relatorio-carga",
		"--cron", "0 10 * * *",
		"--command", "sh scripts/relatorio-carga.sh",
		"--channel", "telegram",
		"--to", "123",
	})
	require.NoError(t, cmd.Execute())

	jobs := cron.NewCronService(store, nil).ListJobs(true)
	require.Len(t, jobs, 1)
	assert.Equal(t, "sh scripts/relatorio-carga.sh", jobs[0].Payload.Command)
	assert.Empty(t, jobs[0].Payload.Message)
	assert.Equal(t, "telegram", jobs[0].Payload.Channel)
	assert.Equal(t, "123", jobs[0].Payload.To)
}

func TestAddCommand_MessageAndCommandMutuallyExclusive(t *testing.T) {
	cmd := newAddCommand(func() string { return filepath.Join(t.TempDir(), "jobs.json") })
	cmd.SetArgs([]string{"--name", "x", "--cron", "0 9 * * *", "--message", "hi", "--command", "true"})
	require.Error(t, cmd.Execute())
}

func TestAddCommand_RequiresMessageOrCommand(t *testing.T) {
	cmd := newAddCommand(func() string { return filepath.Join(t.TempDir(), "jobs.json") })
	cmd.SetArgs([]string{"--name", "x", "--cron", "0 9 * * *"})
	require.Error(t, cmd.Execute())
}

func TestNewAddCommandEveryAndCronMutuallyExclusive(t *testing.T) {
	cmd := newAddCommand(func() string { return "testing" })

	cmd.SetArgs([]string{
		"--name", "job",
		"--message", "hello",
		"--every", "10",
		"--cron", "0 9 * * *",
	})

	err := cmd.Execute()
	require.Error(t, err)
}

// With AC-028-6 dedupe, a second add matching name/schedule/destination
// returns the existing job; attaching a different command to it would
// silently rewrite that job, so it must be refused instead.
func TestAddCommand_SameJobDifferentCommandRefused(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	args := func(command string) []string {
		return []string{
			"--name",
			"r",
			"--cron",
			"0 10 * * *",
			"--command",
			command,
			"--channel",
			"telegram",
			"--to",
			"1",
		}
	}
	first := newAddCommand(func() string { return store })
	first.SetArgs(args("sh a.sh"))
	require.NoError(t, first.Execute())

	second := newAddCommand(func() string { return store })
	second.SetArgs(args("sh b.sh"))
	require.Error(t, second.Execute())

	jobs := cron.NewCronService(store, nil).ListJobs(true)
	require.Len(t, jobs, 1)
	assert.Equal(t, "sh a.sh", jobs[0].Payload.Command)
}

// AC-028-1..3 from the CLI: routines are defined by the operator, not by
// the model, so the options live here and not in the cron tool's schema
// (which would cost tokens on every heartbeat/cron turn).
func TestAddCommand_QuietRoutineWithOnAnomaly(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cmd := newAddCommand(func() string { return store })
	cmd.SetArgs([]string{
		"--name", "rede-4h", "--cron", "0 */4 * * *",
		"--command", "sh scripts/check-rede.sh", "--quiet",
		"--anomaly-regex", "packet loss: [1-9]",
		"--on-anomaly", "Explique a falha de rede ao usuário em 2 frases.",
		"--anomaly-max-per-day", "2",
		"--channel", "telegram", "--to", "1",
	})
	require.NoError(t, cmd.Execute())

	jobs := cron.NewCronService(store, nil).ListJobs(true)
	require.Len(t, jobs, 1)
	p := jobs[0].Payload
	assert.True(t, p.Quiet)
	assert.Equal(t, "packet loss: [1-9]", p.AnomalyRegex)
	require.NotNil(t, p.OnAnomaly)
	assert.Equal(t, "Explique a falha de rede ao usuário em 2 frases.", p.OnAnomaly.Message)
	assert.Equal(t, 2, p.OnAnomaly.MaxPerDay)
}

func TestAddCommand_QuietRequiresCommand(t *testing.T) {
	cmd := newAddCommand(func() string { return filepath.Join(t.TempDir(), "jobs.json") })
	cmd.SetArgs([]string{"--name", "x", "--cron", "0 9 * * *", "--message", "hi", "--quiet"})
	require.Error(t, cmd.Execute())
}

func TestAddCommand_ExistingJobOptionsNotMutated(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	args := []string{
		"--name",
		"r",
		"--cron",
		"0 10 * * *",
		"--command",
		"sh a.sh",
		"--channel",
		"telegram",
		"--to",
		"1",
	}
	first := newAddCommand(func() string { return store })
	first.SetArgs(args)
	require.NoError(t, first.Execute())

	second := newAddCommand(func() string { return store })
	second.SetArgs(append(append([]string{}, args...), "--quiet"))
	require.NoError(t, second.Execute())

	jobs := cron.NewCronService(store, nil).ListJobs(true)
	require.Len(t, jobs, 1)
	assert.False(t, jobs[0].Payload.Quiet, "re-adding an existing job changed its options")
}
