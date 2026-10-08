package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewListbuiltinSubcommand(t *testing.T) {
	cmd := newListBuiltinCommand(nil)

	require.NotNil(t, cmd)

	assert.Equal(t, "list-builtin", cmd.Use)
	assert.Equal(t, "List available builtin skills", cmd.Short)

	assert.Nil(t, cmd.Run)
	assert.NotNil(t, cmd.RunE)

	assert.True(t, cmd.HasExample())
	assert.False(t, cmd.HasSubCommands())

	assert.False(t, cmd.HasFlags())

	assert.Len(t, cmd.Aliases, 0)
}
