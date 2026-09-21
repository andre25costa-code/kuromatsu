package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestConfigPathIgnoresWorkspaceShadow(t *testing.T) {
	home := t.TempDir()
	workspace := filepath.Join(home, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "config.json"), []byte(`{"obsolete":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvHome, home)
	t.Setenv(config.EnvConfig, "")
	t.Chdir(workspace)
	if got := GetConfigPath(); got != filepath.Join(home, "config.json") {
		t.Fatalf("selected shadow config: %s", got)
	}
	t.Setenv(config.EnvConfig, filepath.Join(home, "explicit.json"))
	if got := GetConfigPath(); got != filepath.Join(home, "explicit.json") {
		t.Fatalf("explicit config ignored: %s", got)
	}
}
