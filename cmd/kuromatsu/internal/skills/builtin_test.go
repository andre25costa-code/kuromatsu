package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu"
)

// N08 (audit round 2): install-builtin copied a fixed list (weather, news,
// stock, calculator -- three never existed) from ./kuromatsu/skills, a
// directory no install has, and printed success anyway; list-builtin read
// another missing directory. The builtin skills are the ones embedded in the
// binary (kuromatsu.OnboardWorkspace, workspace/skills).
func TestBuiltinSkills_ListsTheEmbeddedSkills(t *testing.T) {
	list, err := builtinSkills(kuromatsu.OnboardWorkspace)
	require.NoError(t, err)

	byName := map[string]string{}
	for _, s := range list {
		byName[s.Name] = s.Description
	}
	assert.Contains(t, byName, "weather")
	assert.Contains(t, byName, "kuromatsu-docs")
	assert.NotContains(t, byName, "news")
	assert.Contains(t, byName["weather"], "weather")
}

func TestInstallBuiltinSkills_CopiesOnlyMissingSkills(t *testing.T) {
	workspace := t.TempDir()
	mine := filepath.Join(workspace, "skills", "weather", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(mine), 0o755))
	require.NoError(t, os.WriteFile(mine, []byte("my weather"), 0o644))

	installed, kept, err := installBuiltinSkills(kuromatsu.OnboardWorkspace, workspace)
	require.NoError(t, err)

	assert.Contains(t, kept, "weather")
	assert.NotContains(t, installed, "weather")
	assert.Contains(t, installed, "kuromatsu-docs")
	got, _ := os.ReadFile(mine)
	assert.Equal(t, "my weather", string(got), "an installed skill was overwritten")
	_, err = os.Stat(filepath.Join(workspace, "skills", "kuromatsu-docs", "SKILL.md"))
	require.NoError(t, err)

	installed, _, err = installBuiltinSkills(kuromatsu.OnboardWorkspace, workspace)
	require.NoError(t, err)
	assert.Empty(t, installed, "second run must find everything installed")
}
