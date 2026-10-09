package kuromatsu

import (
	"io/fs"
	"os/exec"
	"strings"
	"testing"
)

// N20 (audit round 2): workspace/memory and workspace/skills are embedded as
// whole directories, so a local file that git does not track (notes, a
// personal skill, a secret) would ship inside the binary. Every embedded
// file must be tracked by git.
func TestOnboardWorkspace_EmbedsOnlyTrackedFiles(t *testing.T) {
	out, err := exec.Command("git", "ls-files", "workspace").Output()
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	tracked := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		tracked[strings.TrimSpace(f)] = true
	}

	err = fs.WalkDir(OnboardWorkspace, "workspace", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !tracked[p] {
			t.Errorf("embedded file is not tracked by git: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
