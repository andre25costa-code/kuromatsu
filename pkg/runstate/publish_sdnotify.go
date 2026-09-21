package runstate

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// envNotifySocket is the systemd notify-protocol env var (man sd_notify(3)).
const envNotifySocket = "NOTIFY_SOCKET"

// envWatchdogUsec is set by systemd itself when the unit configures
// WatchdogSec>0 (man sd_notify(3)) -- the interval lives in the unit file,
// not duplicated as a Go constant.
const envWatchdogUsec = "WATCHDOG_USEC"

// SdNotify sends msg as a single datagram to $NOTIFY_SOCKET. A leading "@"
// in the address (Linux abstract socket namespace) is translated to the
// literal NUL byte the kernel expects. Returns nil (a deliberate no-op)
// when NOTIFY_SOCKET isn't set -- not running under systemd, or a unit
// without NotifyAccess, is not a failure to report. On platforms without
// unixgram sockets (e.g. Windows), Dial itself returns an error, which
// callers here treat exactly like "socket unreachable" -- best-effort
// observability, never a reason to change runstate's own behavior.
func SdNotify(msg string) error {
	addr := strings.TrimSpace(os.Getenv(envNotifySocket))
	if addr == "" {
		return nil
	}
	if strings.HasPrefix(addr, "@") {
		addr = "\x00" + addr[1:]
	}

	conn, err := net.Dial("unixgram", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(msg))
	return err
}

// SdReady sends READY=1 (systemd Type=notify: startup finished).
func SdReady() error {
	return SdNotify("READY=1")
}

// SdStopping sends STOPPING=1 (a graceful shutdown began).
func SdStopping() error {
	return SdNotify("STOPPING=1")
}

// RunSdWatchdogPinger sends WATCHDOG=1 at half the interval systemd
// configured via WatchdogSec (WATCHDOG_USEC), as man sd_notify(3)
// recommends, until ctx is done. A no-op when WATCHDOG_USEC is unset or
// unparseable -- running without WatchdogSec configured (or not under
// systemd at all) is not a failure to report, same convention as
// SdNotify's own NOTIFY_SOCKET check.
//
// This is a plain liveness ping on its own ticker, deliberately independent
// of runstate transitions: a genuinely idle agent (heartbeat can be an hour
// apart) must never be killed by the watchdog just for having nothing to
// do. What it does catch is the Go runtime itself getting stuck -- and a
// long llama_decode call in another goroutine does not stop this ticker,
// since cgo calls each run on their own OS thread (ADR-003's one-context
// mutex already serializes the *calls*, not the scheduler).
func RunSdWatchdogPinger(ctx context.Context) {
	usec, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(envWatchdogUsec)), 10, 64)
	if err != nil || usec <= 0 {
		return
	}
	interval := time.Duration(usec) * time.Microsecond / 2
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := SdNotify("WATCHDOG=1"); err != nil {
				logger.DebugCF("runstate", "sd_notify WATCHDOG=1 failed", map[string]any{"error": err.Error()})
			}
		}
	}
}

// RunSdNotifyPublisher sends STATUS=<names> for the current state and every
// subsequent transition, until ctx is done. Errors (most commonly no
// NOTIFY_SOCKET, or a platform without unixgram) are logged at debug level
// and otherwise ignored. Started only when runstate.enabled=true and
// sd_notify is on (gateway.go, C2) -- with either off this is never
// called, so no goroutine/socket traffic exists (AC-017-7).
func RunSdNotifyPublisher(ctx context.Context, e *Engine) {
	if e == nil {
		return
	}
	ch, cancel := e.Subscribe()
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case mode, ok := <-ch:
			if !ok {
				return
			}
			if err := SdNotify("STATUS=" + mode.String()); err != nil {
				logger.DebugCF("runstate", "sd_notify failed", map[string]any{"error": err.Error()})
			}
		}
	}
}
