//go:build !windows

// sd_notify uses unixgram sockets, which don't exist on Windows -- see the
// same note in pkg/runstate/publish_sdnotify_test.go, whose
// listenUnixgram/readOneDatagram pattern this mirrors (unexported, so not
// reusable across packages).

package gateway

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSdReadyOnStartup_FiresRegardlessOfConfig and the STOPPING= sibling
// below cover the fix alongside sdReadyOnStartup/sdStoppingOnShutdown's own
// doc comments: Type=notify in the deploy unit (kuromatsu.service) makes
// sending READY=1/STOPPING=1 a systemd contract the process must honor
// unconditionally -- gating it on cfg.Runstate (the old sdReadyIfEnabled)
// would leave a unit with runstate disabled stuck "activating" until
// TimeoutStartSec kills it, even though the gateway is up and fine.
func TestSdReadyOnStartup_FiresRegardlessOfConfig(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "notify.sock")
	listener := listenUnixgramForTest(t, sockPath)
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)

	sdReadyOnStartup()

	if got := readOneDatagramForTest(t, listener); got != "READY=1" {
		t.Fatalf("datagram = %q, want %q", got, "READY=1")
	}
}

func TestSdStoppingOnShutdown_FiresRegardlessOfConfig(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "notify.sock")
	listener := listenUnixgramForTest(t, sockPath)
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)

	sdStoppingOnShutdown()

	if got := readOneDatagramForTest(t, listener); got != "STOPPING=1" {
		t.Fatalf("datagram = %q, want %q", got, "STOPPING=1")
	}
}

func listenUnixgramForTest(t *testing.T, path string) *net.UnixConn {
	t.Helper()
	_ = os.Remove(path)
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatalf("ListenUnixgram(%s): %v", path, err)
	}
	return conn
}

func readOneDatagramForTest(t *testing.T, conn *net.UnixConn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("reading datagram: %v", err)
	}
	return string(buf[:n])
}
