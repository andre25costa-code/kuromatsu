package pid

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// tmpDir returns a clean temporary directory for a test.
func tmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pidtest-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestGenerateToken verifies that generateToken produces a 32-character hex string.
func TestGenerateToken(t *testing.T) {
	token := generateToken()
	if len(token) != 32 {
		t.Errorf("expected token length 32, got %d (token: %q)", len(token), token)
	}
	// Verify all characters are valid hex.
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("token contains non-hex character: %c", c)
		}
	}
}

// TestGenerateTokenUniqueness checks that two consecutive tokens differ.
func TestGenerateTokenUniqueness(t *testing.T) {
	a := generateToken()
	b := generateToken()
	if a == b {
		t.Error("two consecutive tokens should not be equal")
	}
}

// TestPidFilePath returns the expected path.
func TestPidFilePath(t *testing.T) {
	dir := tmpDir(t)
	got := pidFilePath(dir)
	want := filepath.Join(dir, pidFileName)
	if got != want {
		t.Errorf("pidFilePath(%q) = %q, want %q", dir, got, want)
	}
}

// TestWritePidFile creates a PID file and verifies its contents.
func TestWritePidFile(t *testing.T) {
	dir := tmpDir(t)
	data, err := WritePidFile(dir, "127.0.0.1", 18790)
	if err != nil {
		t.Fatalf("WritePidFile failed: %v", err)
	}

	if data.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", data.PID, os.Getpid())
	}
	if data.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", data.Host, "127.0.0.1")
	}
	if data.Port != 18790 {
		t.Errorf("Port = %d, want %d", data.Port, 18790)
	}
	if len(data.Token) != 32 {
		t.Errorf("Token length = %d, want 32", len(data.Token))
	}

	// Verify the file exists and can be unmarshalled.
	raw, err := os.ReadFile(filepath.Join(dir, pidFileName))
	if err != nil {
		t.Fatalf("failed to read pid file: %v", err)
	}

	var fileData PidFileData
	if err = json.Unmarshal(raw, &fileData); err != nil {
		t.Fatalf("failed to unmarshal pid file: %v", err)
	}
	if fileData.PID != data.PID || fileData.Token != data.Token {
		t.Error("file data mismatch")
	}

	// Verify file permissions (owner-only read/write).
	info, err := os.Stat(filepath.Join(dir, pidFileName))
	if err != nil {
		t.Fatalf("failed to stat pid file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file permission = %o, want 0600", perm)
	}
}

// TestWritePidFileOverwrite writes twice and verifies the PID file is replaced.
func TestWritePidFileOverwrite(t *testing.T) {
	dir := tmpDir(t)

	data1, err := WritePidFile(dir, "0.0.0.0", 18790)
	if err != nil {
		t.Fatalf("first WritePidFile failed: %v", err)
	}

	// Second write should succeed because the PID matches our process.
	data2, err := WritePidFile(dir, "0.0.0.0", 18800)
	if err != nil {
		t.Fatalf("second WritePidFile failed: %v", err)
	}

	if data2.Token == data1.Token {
		t.Error("token should change on re-write")
	}
	if data2.Port != 18800 {
		t.Errorf("Port = %d, want 18800", data2.Port)
	}
}

// TestWritePidFileStalePID writes a PID file with a non-running PID, then
// verifies WritePidFile cleans it up and writes a new one.
func TestWritePidFileStalePID(t *testing.T) {
	dir := tmpDir(t)

	// Write a PID file with a PID that almost certainly doesn't exist.
	stale := PidFileData{PID: 99999999, Token: "deadbeef12345678deadbeef12345678"}
	raw, _ := json.MarshalIndent(stale, "", "  ")
	os.WriteFile(filepath.Join(dir, pidFileName), raw, 0o600)

	data, err := WritePidFile(dir, "127.0.0.1", 18790)
	if err != nil {
		t.Fatalf("WritePidFile with stale PID failed: %v", err)
	}
	if data.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", data.PID, os.Getpid())
	}
}

// TestRemovePidFile removes the PID file for the current process.
func TestRemovePidFile(t *testing.T) {
	dir := tmpDir(t)

	if _, err := WritePidFile(dir, "127.0.0.1", 18790); err != nil {
		t.Fatalf("WritePidFile failed: %v", err)
	}

	RemovePidFile(dir)

	if _, err := os.Stat(filepath.Join(dir, pidFileName)); !os.IsNotExist(err) {
		t.Error("PID file should be removed")
	}
}

// TestRemovePidFileDifferentPID does not remove a PID file owned by another process.
func TestRemovePidFileDifferentPID(t *testing.T) {
	dir := tmpDir(t)

	other := PidFileData{PID: 99999999, Token: "deadbeef12345678deadbeef12345678"}
	raw, _ := json.MarshalIndent(other, "", "  ")
	os.WriteFile(filepath.Join(dir, pidFileName), raw, 0o600)

	RemovePidFile(dir)

	if _, err := os.Stat(filepath.Join(dir, pidFileName)); os.IsNotExist(err) {
		t.Error("PID file should NOT be removed (different PID)")
	}
}

// TestRemovePidFileNonexistent does not error on missing file.
func TestRemovePidFileNonexistent(t *testing.T) {
	dir := tmpDir(t)
	// Should not panic or error.
	RemovePidFile(dir)
}

// TestWritePidFileContainerPID1 verifies that a leftover PID file with PID 1
// (typical container entrypoint) is treated as stale and overwritten.
func TestWritePidFileContainerPID1(t *testing.T) {
	dir := tmpDir(t)

	stale := PidFileData{PID: 1, Token: "deadbeef12345678deadbeef12345678"}
	raw, _ := json.MarshalIndent(stale, "", "  ")
	os.WriteFile(filepath.Join(dir, pidFileName), raw, 0o600)

	data, err := WritePidFile(dir, "127.0.0.1", 18790)
	if err != nil {
		t.Fatalf("WritePidFile should treat PID 1 as stale, got error: %v", err)
	}
	if data.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", data.PID, os.Getpid())
	}
}

// TestReadPidFileUnlockedInvalidJSON returns error for malformed content.
func TestReadPidFileUnlockedInvalidJSON(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, pidFileName)
	os.WriteFile(path, []byte("not json"), 0o600)

	_, err := readPidFileUnlocked(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestReadPidFileUnlockedInvalidPID returns error for non-positive PID.
func TestReadPidFileUnlockedInvalidPID(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, pidFileName)
	os.WriteFile(path, []byte(`{"pid": -1, "token": "a"}`), 0o600)

	_, err := readPidFileUnlocked(path)
	if err == nil {
		t.Error("expected error for invalid PID")
	}
}

// TestRunningGateway_StaleAndPID1 checks that a dead PID and a recorded
// PID 1 are not reported as a running gateway, that the file is left in
// place, and that a live process named kuromatsu is reported.
func TestRunningGateway_StaleAndPID1(t *testing.T) {
	write := func(t *testing.T, dir string, pid int) {
		t.Helper()
		raw, _ := json.Marshal(PidFileData{PID: pid})
		if err := os.WriteFile(filepath.Join(dir, pidFileName), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no file", func(t *testing.T) {
		if pid, ok := RunningGateway(tmpDir(t)); ok {
			t.Fatalf("RunningGateway = %d, true; want false", pid)
		}
	})

	for _, tc := range []struct {
		name string
		pid  int
	}{
		{"stale", 99999999},
		{"pid 1", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tmpDir(t)
			write(t, dir, tc.pid)
			if pid, ok := RunningGateway(dir); ok {
				t.Fatalf("RunningGateway = %d, true; want false", pid)
			}
			if _, err := os.Stat(filepath.Join(dir, pidFileName)); err != nil {
				t.Fatalf("pid file must be left in place: %v", err)
			}
		})
	}

	t.Run("live kuromatsu", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("process name check via /proc is Linux-only")
		}
		sleep, err := exec.LookPath("sleep")
		if err != nil {
			t.Skip("no sleep binary")
		}
		dir := tmpDir(t)
		// /proc/<pid>/comm holds the first 15 bytes of the executable name.
		bin := filepath.Join(dir, "kuromatsu-sleep")
		src, err := os.ReadFile(sleep)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(bin, src, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "30")
		if err = cmd.Start(); err != nil {
			t.Skipf("cannot start %s: %v", bin, err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		write(t, dir, cmd.Process.Pid)

		pid, ok := RunningGateway(dir)
		if !ok || pid != cmd.Process.Pid {
			t.Fatalf("RunningGateway = %d, %v; want %d, true", pid, ok, cmd.Process.Pid)
		}
	})
}
