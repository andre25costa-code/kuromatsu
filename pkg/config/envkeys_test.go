package config

import (
	"os"
	"path/filepath"
	"testing"
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
	os.Unsetenv("PICOCLAW_HOME")

	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome) // os.UserHomeDir() on Windows

	oldHome := filepath.Join(userHome, ".picoclaw")
	if err := os.MkdirAll(oldHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// deliberately no .kuromatsu dir created

	if got := GetHome(); got != oldHome {
		t.Fatalf("GetHome() = %q, want the pre-existing %q", got, oldHome)
	}
}

func TestGetHome_PrefersKuromatsuHome_WhenBothDirsExist(t *testing.T) {
	os.Unsetenv(EnvHome)
	os.Unsetenv("PICOCLAW_HOME")

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
	os.Unsetenv("PICOCLAW_HOME")

	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)

	want := filepath.Join(userHome, ".kuromatsu")
	if got := GetHome(); got != want {
		t.Fatalf("GetHome() = %q, want %q", got, want)
	}
}
