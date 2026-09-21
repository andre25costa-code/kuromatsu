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

func TestRunSdWatchdogPinger_NoIntervalConfiguredIsNoOp(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "notify.sock")
	listener := listenUnixgram(t, sockPath)
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)
	t.Setenv("WATCHDOG_USEC", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunSdWatchdogPinger(ctx)

	// NOTIFY_SOCKET is a process-wide env var that SdNotify re-reads on
	// every call, so a goroutine from an unrelated, already-finished test
	// (e.g. TestRunSdNotifyPublisher_SendsStatusOnTransitions's publisher,
	// mid-unwind past its own test's cancel()) can occasionally still land
	// an unrelated datagram on whatever socket this test happens to be
	// pointed at when it fires -- observed for real on this suite. Drain
	// and ignore anything that isn't the one message this test actually
	// cares about, rather than failing on the first datagram of any kind.
	deadline := time.Now().Add(200 * time.Millisecond)
	buf := make([]byte, 64)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		_ = listener.SetReadDeadline(deadline)
		n, err := listener.Read(buf)
		if err != nil {
			return // deadline hit waiting for a next datagram -- pass
		}
		if got := string(buf[:n]); got == "WATCHDOG=1" {
			t.Fatalf("received %q with WATCHDOG_USEC unset", got)
		}
	}
}

func TestRunSdWatchdogPinger_PingsAtHalfTheConfiguredInterval(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "notify.sock")
	listener := listenUnixgram(t, sockPath)
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)
	// 100ms WatchdogSec -> systemd would set WATCHDOG_USEC=100000; pinging
	// at half that (50ms) leaves comfortable margin to observe two pings
	// inside this test's own timeout.
	t.Setenv("WATCHDOG_USEC", "100000")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunSdWatchdogPinger(ctx)

	for i := 0; i < 2; i++ {
		if got := readOneDatagram(t, listener); got != "WATCHDOG=1" {
			t.Fatalf("datagram #%d = %q, want %q", i, got, "WATCHDOG=1")
		}
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
