//go:build !windows

// sd_notify uses unixgram sockets, which don't exist on Windows -- this
// whole file (production code included, see publish_sdnotify.go's use of
// net.Dial("unixgram", ...)) is exercised on Linux/macOS only. On Windows,
// SdNotify still compiles and runs (net.Dial just returns an error that is
// logged and otherwise ignored, per its doc comment); only this test file
// needs the real listener to assert against, which unixgram sockets don't
// support there.

package runstate

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSdNotify_NoSocketConfiguredIsNoOp(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := SdNotify("READY=1"); err != nil {
		t.Fatalf("SdNotify with no NOTIFY_SOCKET: err = %v, want nil", err)
	}
}

func TestSdNotify_SendsDatagramToConfiguredSocket(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "notify.sock")
	listener := listenUnixgram(t, sockPath)
	defer listener.Close()

	t.Setenv("NOTIFY_SOCKET", sockPath)
	if err := SdNotify("STATUS=inference"); err != nil {
		t.Fatalf("SdNotify: %v", err)
	}

	got := readOneDatagram(t, listener)
	if got != "STATUS=inference" {
		t.Fatalf("datagram = %q, want %q", got, "STATUS=inference")
	}
}

func TestSdReadyAndSdStopping(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "notify.sock")
	listener := listenUnixgram(t, sockPath)
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)

	if err := SdReady(); err != nil {
		t.Fatalf("SdReady: %v", err)
	}
	if got := readOneDatagram(t, listener); got != "READY=1" {
		t.Fatalf("datagram = %q, want %q", got, "READY=1")
	}

	if err := SdStopping(); err != nil {
		t.Fatalf("SdStopping: %v", err)
	}
	if got := readOneDatagram(t, listener); got != "STOPPING=1" {
		t.Fatalf("datagram = %q, want %q", got, "STOPPING=1")
	}
}

func TestRunSdNotifyPublisher_SendsStatusOnTransitions(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "notify.sock")
	listener := listenUnixgram(t, sockPath)
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)

	e := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunSdNotifyPublisher(ctx, e)

	if got := readOneDatagram(t, listener); got != "STATUS=idle" {
		t.Fatalf("initial datagram = %q, want %q", got, "STATUS=idle")
	}

	release := e.Enter(Reflex)
	defer release()
	if got := readOneDatagram(t, listener); got != "STATUS=reflex" {
		t.Fatalf("transition datagram = %q, want %q", got, "STATUS=reflex")
	}
}

func listenUnixgram(t *testing.T, path string) *net.UnixConn {
	t.Helper()
	_ = os.Remove(path)
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatalf("ListenUnixgram(%s): %v", path, err)
	}
	return conn
}

func readOneDatagram(t *testing.T, conn *net.UnixConn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("reading datagram: %v", err)
	}
	return string(buf[:n])
}
