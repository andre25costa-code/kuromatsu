package onboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/hwprofile"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
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

func withDetectedHardware(t *testing.T, hw sysinfo.Hardware) {
	t.Helper()
	old := detect
	detect = func() sysinfo.Hardware { return hw }
	t.Cleanup(func() { detect = old })
}

func readSavedConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func nativeNCtx(t *testing.T, m map[string]any) any {
	t.Helper()
	for _, raw := range m["model_list"].([]any) {
		entry := raw.(map[string]any)
		if entry["model_name"] == config.NativeModelName {
			return entry["extra_body"].(map[string]any)["n_ctx"]
		}
	}
	t.Fatal("native entry missing from the saved config")
	return nil
}

// A new config (empty home) gets the detected hardware profile written as
// explicit values.
func TestOnboard_FreshConfigAppliesDetectedProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	configPath := filepath.Join(home, "config.json")
	t.Setenv(config.EnvConfig, configPath)
	withDetectedHardware(t, sysinfo.Hardware{TotalRAM: 512 << 20, CPUs: 1, NativeCPUOK: true})

	onboard(false, false, "")

	m := readSavedConfig(t, configPath)
	if got := m["heartbeat"].(map[string]any)["interval"]; got != float64(60) {
		t.Fatalf("heartbeat.interval = %v, want 60 (Ultra-Light)", got)
	}
	if got := nativeNCtx(t, m); got != float64(1024) {
		t.Fatalf("native n_ctx = %v, want 1024 (Ultra-Light)", got)
	}
}

// An existing config changes only with --profile.
func TestOnboard_ExistingConfigProfileFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	configPath := filepath.Join(home, "config.json")
	t.Setenv(config.EnvConfig, configPath)
	withDetectedHardware(t, sysinfo.Hardware{TotalRAM: 990 << 20, CPUs: 2, NativeCPUOK: true})

	onboard(false, false, "") // fresh: Light
	cfg, err := config.LoadConfigForEdit(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Heartbeat.Interval = 45
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	onboard(false, false, "") // existing, no flag: untouched
	if got := readSavedConfig(t, configPath)["heartbeat"].(map[string]any)["interval"]; got != float64(45) {
		t.Fatalf("existing config changed without --profile: interval = %v", got)
	}

	onboard(false, false, "performance")
	m := readSavedConfig(t, configPath)
	if got := m["heartbeat"].(map[string]any)["interval"]; got != float64(15) {
		t.Fatalf("--profile performance: interval = %v, want 15", got)
	}
	if got := nativeNCtx(t, m); got != float64(8192) {
		t.Fatalf("--profile performance: n_ctx = %v, want 8192", got)
	}

	onboard(false, false, "auto")
	if got := nativeNCtx(t, readSavedConfig(t, configPath)); got != float64(2048) {
		t.Fatalf("--profile auto on 990 MiB: n_ctx = %v, want 2048 (Light)", got)
	}
}

func TestParseProfileFlag(t *testing.T) {
	if n, auto, err := parseProfileFlag(""); n != "" || auto || err != nil {
		t.Fatalf("empty flag: %q %v %v", n, auto, err)
	}
	if _, auto, err := parseProfileFlag("AUTO"); !auto || err != nil {
		t.Fatalf("auto: %v %v", auto, err)
	}
	if n, _, err := parseProfileFlag("ultra-light"); n != hwprofile.UltraLight || err != nil {
		t.Fatalf("ultra-light: %q %v", n, err)
	}
	if _, _, err := parseProfileFlag("custom"); err == nil {
		t.Fatal("custom accepted")
	}
}
