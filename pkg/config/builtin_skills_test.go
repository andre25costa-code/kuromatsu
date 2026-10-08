package config

import (
	"os"
	"path/filepath"
	"testing"
)

// N09 (audit round 2): the builtin skills directory had five definitions
// (<cwd>/skills in the agent and in evolution, with different Getwd
// fallbacks; <home>/kuromatsu/skills in the CLI). The builtin skills ship
// in the binary and are copied into the workspace by onboard and
// install-builtin, so no directory exists by default -- a skills/ folder in
// whatever directory kuromatsu was started from must not be loaded.
// KUROMATSU_BUILTIN_SKILLS still names one explicitly.
func TestBuiltinSkillsDir(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	t.Setenv(EnvBuiltinSkills, "")
	if got := BuiltinSkillsDir(); got != "" {
		t.Fatalf("BuiltinSkillsDir() = %q without %s, want none", got, EnvBuiltinSkills)
	}

	t.Setenv(EnvBuiltinSkills, " /opt/skills ")
	if got := BuiltinSkillsDir(); got != "/opt/skills" {
		t.Fatalf("BuiltinSkillsDir() = %q, want /opt/skills", got)
	}
}
