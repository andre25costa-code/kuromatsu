package onboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyEmbeddedToTargetUsesStructuredAgentFiles(t *testing.T) {
	targetDir := t.TempDir()

	if _, err := copyEmbeddedToTarget(targetDir, false); err != nil {
		t.Fatalf("copyEmbeddedToTarget() error = %v", err)
	}

	agentPath := filepath.Join(targetDir, "AGENT.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Fatalf("expected %s to exist: %v", agentPath, err)
	}

	soulPath := filepath.Join(targetDir, "SOUL.md")
	if _, err := os.Stat(soulPath); err != nil {
		t.Fatalf("expected %s to exist: %v", soulPath, err)
	}

	userPath := filepath.Join(targetDir, "USER.md")
	if _, err := os.Stat(userPath); err != nil {
		t.Fatalf("expected %s to exist: %v", userPath, err)
	}

	for _, legacyName := range []string{"AGENTS.md", "IDENTITY.md"} {
		legacyPath := filepath.Join(targetDir, legacyName)
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("expected legacy file %s to be absent, got err=%v", legacyPath, err)
		}
	}
}

// N01 (audit round 2): re-running onboard (e.g. `onboard --enc` to add
// encryption to an existing install) overwrote SOUL.md, USER.md, HEARTBEAT.md
// and memory/MEMORY.md with the templates. Existing files are kept; only
// missing templates are written, unless the caller asks to overwrite.
func TestCopyEmbeddedToTarget_KeepsExistingWorkspaceFiles(t *testing.T) {
	targetDir := t.TempDir()
	soul := filepath.Join(targetDir, "SOUL.md")
	memory := filepath.Join(targetDir, "memory", "MEMORY.md")
	if err := os.MkdirAll(filepath.Dir(memory), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{soul, memory} {
		if err := os.WriteFile(p, []byte("user content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := copyEmbeddedToTarget(targetDir, false)
	if err != nil {
		t.Fatalf("copyEmbeddedToTarget() error = %v", err)
	}
	for _, p := range []string{soul, memory} {
		if got, _ := os.ReadFile(p); string(got) != "user content" {
			t.Fatalf("%s overwritten: %q", p, got)
		}
	}
	if len(res.kept) != 2 {
		t.Fatalf("kept = %v, want SOUL.md and memory/MEMORY.md", res.kept)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "AGENT.md")); err != nil {
		t.Fatalf("missing template not created: %v", err)
	}

	if _, err := copyEmbeddedToTarget(targetDir, true); err != nil {
		t.Fatalf("copyEmbeddedToTarget(overwrite) error = %v", err)
	}
	if got, _ := os.ReadFile(soul); string(got) == "user content" {
		t.Fatal("overwrite did not restore the template")
	}
}

// M09 (audit round 2): onboard installed every builtin skill, including ones
// whose tools do not exist on this machine (gh, tmux, agent-browser,
// summarize on Windows, Termux or Docker), and they all entered the agent's
// skill catalog. onboard now installs only skills whose required tools are on
// PATH and whose OS list (if any) includes this OS, and reports the rest;
// `skills install-builtin` still installs any of them on request.
func TestCopyEmbeddedToTarget_SkipsSkillsWithMissingRequirements(t *testing.T) {
	savedLook, savedOS := lookPath, goos
	defer func() { lookPath, goos = savedLook, savedOS }()
	lookPath = func(bin string) (string, error) {
		if bin == "curl" || bin == "tmux" {
			return "/usr/bin/" + bin, nil
		}
		return "", os.ErrNotExist
	}
	goos = "windows" // tmux is found, but its skill is for darwin/linux only

	targetDir := t.TempDir()
	res, err := copyEmbeddedToTarget(targetDir, false)
	if err != nil {
		t.Fatalf("copyEmbeddedToTarget() error = %v", err)
	}
	for _, name := range []string{"weather", "kuromatsu-docs", "skill-creator"} {
		if _, err := os.Stat(filepath.Join(targetDir, "skills", name, "SKILL.md")); err != nil {
			t.Errorf("skill %s should be installed: %v", name, err)
		}
	}
	for _, name := range []string{"github", "agent-browser", "summarize", "tmux"} {
		if _, err := os.Stat(filepath.Join(targetDir, "skills", name)); !os.IsNotExist(err) {
			t.Errorf("skill %s should be skipped, got err=%v", name, err)
		}
		if len(res.skipped[name]) == 0 {
			t.Errorf("skill %s not reported as skipped: %v", name, res.skipped)
		}
	}
}
