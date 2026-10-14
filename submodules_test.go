package kuromatsu

import (
	"os/exec"
	"strings"
	"testing"
)

// Every submodule declared in .gitmodules must have a gitlink (mode 160000)
// in the index; otherwise `git submodule update --init` silently does nothing
// on a fresh clone. The index is checked rather than HEAD so a staged removal
// is caught before it is committed.
func TestSubmodules_HaveGitlinks(t *testing.T) {
	out, err := exec.Command("git", "config", "-f", ".gitmodules", "--get-regexp", `^submodule\..*\.path$`).Output()
	if err != nil {
		t.Skipf("git not available or no submodules declared: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("unexpected git config line: %q", line)
		}
		path := fields[1]
		stage, err := exec.Command("git", "ls-files", "--stage", "--", path).Output()
		if err != nil {
			t.Fatalf("git ls-files --stage -- %s: %v", path, err)
		}
		if !strings.HasPrefix(string(stage), "160000 ") {
			t.Errorf(
				"submodule %q is declared in .gitmodules but has no gitlink (git ls-files --stage is %q)",
				path, strings.TrimSpace(string(stage)),
			)
		}
	}
}
