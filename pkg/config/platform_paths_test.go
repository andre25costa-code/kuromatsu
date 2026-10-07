package config

import (
	"runtime"
	"testing"
)

func TestForeignWorkspaceRejectedOnLinux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux path guard")
	}
	for _, path := range []string{`C:\Users\pc\workspace`, "C:/Users/pc/workspace", `\\server\share`} {
		cfg := DefaultConfig()
		cfg.Agents.Defaults.Workspace = path
		if cfg.ValidatePlatformPaths() == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	cfg := DefaultConfig()
	cfg.Agents.Defaults.Workspace = "/var/lib/kuromatsu/workspace"
	if err := cfg.ValidatePlatformPaths(); err != nil {
		t.Fatal(err)
	}
}
