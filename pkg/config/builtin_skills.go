package config

import (
	"os"
	"strings"
)

// BuiltinSkillsDir is the extra, lowest-priority skills directory: the one
// named by KUROMATSU_BUILTIN_SKILLS, or none. The builtin skills ship inside
// the binary and are copied into the workspace by `onboard` and
// `skills install-builtin`; a skills/ folder in whatever directory kuromatsu
// was started from is not loaded.
func BuiltinSkillsDir() string {
	return strings.TrimSpace(os.Getenv(EnvBuiltinSkills))
}
