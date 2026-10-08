package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

func TestGetHome_ExplicitEnvVarWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvHome, dir)

	if got := GetHome(); got != dir {
		t.Fatalf("GetHome() = %q, want %q", got, dir)
	}
}

func TestGetHome_ReadsPicoclawHomeInPlace_WhenKuromatsuHomeMissing(t *testing.T) {
	os.Unsetenv(EnvHome)
	os.Unsetenv("KUROMATSU_HOME")

	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome) // os.UserHomeDir() on Windows

	oldHome := filepath.Join(userHome, ".picoclaw")
	if err := os.MkdirAll(oldHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// deliberately no .kuromatsu dir created

	// AC-008-2: reading the pre-rebrand home is announced in the log.
	logPath := filepath.Join(t.TempDir(), "home.log")
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatal(err)
	}
	defer logger.DisableFileLogging()

	if got := GetHome(); got != oldHome {
		t.Fatalf("GetHome() = %q, want the pre-existing %q", got, oldHome)
	}
	if logged, _ := os.ReadFile(logPath); !strings.Contains(string(logged), ".picoclaw") {
		t.Fatalf("reading the old home was not logged:\n%s", logged)
	}
}

func TestGetHome_PrefersKuromatsuHome_WhenBothDirsExist(t *testing.T) {
	os.Unsetenv(EnvHome)
	os.Unsetenv("KUROMATSU_HOME")

	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)

	newHome := filepath.Join(userHome, ".kuromatsu")
	oldHome := filepath.Join(userHome, ".picoclaw")
	if err := os.MkdirAll(newHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oldHome, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := GetHome(); got != newHome {
		t.Fatalf("GetHome() = %q, want the new home %q once it exists", got, newHome)
	}
}

func TestGetHome_DefaultsToKuromatsuHome_WhenNeitherDirExists(t *testing.T) {
	os.Unsetenv(EnvHome)
	os.Unsetenv("KUROMATSU_HOME")

	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)

	want := filepath.Join(userHome, ".kuromatsu")
	if got := GetHome(); got != want {
		t.Fatalf("GetHome() = %q, want %q", got, want)
	}
}
