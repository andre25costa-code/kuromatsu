package status

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = oldStdout
	defer r.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy() error = %v", err)
	}
	return buf.String()
}

func TestStatusCmd_RecognizesProviderFieldWithoutModelPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	workspace := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	t.Setenv(config.EnvConfig, configPath)
	t.Setenv(config.EnvHome, tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName:   "gpt-5.4",
				Workspace:   workspace,
				Provider:    "openai",
				MaxTokens:   65536,
				Temperature: nil,
			},
		},
		ModelList: []*config.ModelConfig{
			{
				ModelName: "gpt-5.4",
				Provider:  "openai",
				Model:     "gpt-5.4",
				APIBase:   "https://api.openai.com/v1",
				APIKeys:   config.SimpleSecureStrings("test-key"),
				Enabled:   true,
			},
			{
				ModelName: "qwen-plus",
				Provider:  "qwen",
				Model:     "qwen-plus",
				APIBase:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
				APIKeys:   config.SimpleSecureStrings("test-key"),
				Enabled:   true,
			},
		},
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("config.SaveConfig() error = %v", err)
	}

	output := captureStdout(t, statusCmd)

	if !strings.Contains(output, "OpenAI API: \u2713") {
		t.Fatalf("status output missing OpenAI provider: %s", output)
	}
	if !strings.Contains(output, "Qwen API: \u2713") {
		t.Fatalf("status output missing Qwen provider: %s", output)
	}
}

func TestStatus_ShowsHardware(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	t.Setenv(config.EnvConfig, configPath)
	t.Setenv(config.EnvHome, tmpDir)
	if err := config.SaveConfig(configPath, config.DefaultConfig()); err != nil {
		t.Fatalf("config.SaveConfig() error = %v", err)
	}
	old := detect
	detect = func() sysinfo.Hardware {
		return sysinfo.Hardware{
			TotalRAM: 990 << 20, AvailableRAM: 420 << 20, CPUs: 2, Arch: "amd64", OS: "linux",
			NativeCPUOK: true,
		}
	}
	t.Cleanup(func() { detect = old })

	output := captureStdout(t, statusCmd)

	for _, want := range []string{
		"Hardware   0.97 GiB RAM (0.41 GiB available), limit none, 2 CPUs, amd64",
		"Profile    detected: Light · config matches: Light",
		"Native CPU ",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output missing %q:\n%s", want, output)
		}
	}
}

func TestHardwareLines_NativeCPU(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	cfg := config.DefaultConfig()
	ok := sysinfo.Hardware{TotalRAM: 16 << 30, CPUs: 8, Arch: "amd64", NativeCPUOK: true}
	bad := sysinfo.Hardware{
		TotalRAM: 16 << 30, MemLimit: 1 << 30, CPUs: 8, Arch: "amd64",
		NativeCPUMissing: []string{"AVX2", "FMA"},
	}

	cases := []struct {
		hw    sysinfo.Hardware
		built bool
		want  string
	}{
		{ok, true, "Native CPU ✓ AVX2 FMA"},
		{bad, true, "Native CPU ✗ missing AVX2 FMA — native model disabled"},
		{bad, false, "Native CPU n/a (pure-Go build)"},
	}
	for _, tc := range cases {
		lines := hardwareLines(cfg, tc.hw, tc.built)
		if lines[2] != tc.want {
			t.Errorf("built=%v: %q, want %q", tc.built, lines[2], tc.want)
		}
	}

	lines := hardwareLines(cfg, bad, false)
	if !strings.Contains(lines[0], "(available unknown), limit 1.00 GiB") {
		t.Errorf("hardware line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "detected: Light") {
		t.Errorf("profile line = %q (1 GiB cgroup limit should select Light)", lines[1])
	}
	if l := hardwareLines(cfg, sysinfo.Hardware{CPUs: 2}, false); !strings.Contains(l[0], "RAM unknown") ||
		!strings.Contains(l[1], "RAM not detected; using Light") {
		t.Errorf("unknown RAM lines = %q", l)
	}
}
