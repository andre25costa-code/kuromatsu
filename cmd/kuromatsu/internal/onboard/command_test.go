package onboard

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOnboardCommand(t *testing.T) {
	cmd := NewOnboardCommand()

	require.NotNil(t, cmd)

	assert.Equal(t, "onboard", cmd.Use)
	assert.Equal(t, "Initialize kuromatsu configuration and workspace", cmd.Short)

	assert.Len(t, cmd.Aliases, 1)
	assert.True(t, cmd.HasAlias("o"))

	assert.NotNil(t, cmd.Run)
	assert.Nil(t, cmd.RunE)

	assert.Nil(t, cmd.PersistentPreRun)
	assert.Nil(t, cmd.PersistentPostRun)

	assert.True(t, cmd.HasFlags())
	encFlag := cmd.Flags().Lookup("enc")
	require.NotNil(t, encFlag, "expected --enc flag to be registered")
	assert.Equal(t, "false", encFlag.DefValue, "--enc should default to false")
	assert.False(t, cmd.HasSubCommands())
}

func TestOnboardCommand_NonInteractiveExcludesEnc(t *testing.T) {
	cmd := NewOnboardCommand()
	flag := cmd.Flags().Lookup("non-interactive")
	require.NotNil(t, flag, "expected --non-interactive flag to be registered")
	assert.Equal(t, "false", flag.DefValue)

	cmd.SetArgs([]string{"--enc", "--non-interactive"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	require.Error(t, err, "--enc prompts for a passphrase and must be refused with --non-interactive")
	assert.Contains(t, err.Error(), "non-interactive")
}
