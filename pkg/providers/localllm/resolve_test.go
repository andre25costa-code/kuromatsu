package localllm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModelPath_AbsolutePathUsedAsIs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.gguf")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveModelPath(path, "")
	if err != nil {
		t.Fatalf("ResolveModelPath() error = %v", err)
	}
	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}

func TestResolveModelPath_EnvModelsDirTakesPriority(t *testing.T) {
	envDir := t.TempDir()
	homeDir := t.TempDir()

	writeGGUF(t, filepath.Join(envDir, "bonsai.gguf"))
	writeGGUF(t, filepath.Join(homeDir, "models", "bonsai.gguf"))

	t.Setenv(EnvModelsDir, envDir)

	got, err := ResolveModelPath("bonsai", homeDir)
	if err != nil {
		t.Fatalf("ResolveModelPath() error = %v", err)
	}
	if got != filepath.Join(envDir, "bonsai.gguf") {
		t.Fatalf("got %q, want the env-dir path", got)
	}
}

func TestResolveModelPath_FallsBackToHomeModelsDir(t *testing.T) {
	homeDir := t.TempDir()
	want := filepath.Join(homeDir, "models", "bonsai.gguf")
	writeGGUF(t, want)

	got, err := ResolveModelPath("bonsai", homeDir)
	if err != nil {
		t.Fatalf("ResolveModelPath() error = %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveModelPath_AppendsGGUFExtension(t *testing.T) {
	homeDir := t.TempDir()
	writeGGUF(t, filepath.Join(homeDir, "models", "Bonsai-1.7B-Q1_0.gguf"))

	got, err := ResolveModelPath("Bonsai-1.7B-Q1_0", homeDir)
	if err != nil {
		t.Fatalf("ResolveModelPath() error = %v", err)
	}
	if filepath.Base(got) != "Bonsai-1.7B-Q1_0.gguf" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveModelPath_NotFound(t *testing.T) {
	_, err := ResolveModelPath("does-not-exist", t.TempDir())
	if err != ErrModelNotFound {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func TestResolveModelPath_EmptyModelID(t *testing.T) {
	_, err := ResolveModelPath("", "")
	if err != ErrModelNotFound {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func writeGGUF(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
