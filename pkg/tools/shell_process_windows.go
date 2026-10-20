//go:build windows

package tools

import (
	"os/exec"
	"strconv"
)

func prepareCommandForTermination(cmd *exec.Cmd) {
	// no-op on Windows
}

// prepareContextCommandForTermination makes context cancellation kill the
// whole process tree. The default Cancel (Process.Kill) would kill only the
// shell, and once the shell is gone taskkill /T can no longer walk to its
// children, leaving them running.
func prepareContextCommandForTermination(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.Cancel = func() error { return terminateProcessTree(cmd) }
}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	if pid <= 0 {
		return nil
	}

	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
	_ = cmd.Process.Kill()
	return nil
}
