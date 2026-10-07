package runstate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunFilePublisher_WritesStateAndTransitions(t *testing.T) {
	e := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "state")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunFilePublisher(ctx, e, path)
		close(done)
	}()
	// Cancel and wait for the goroutine to actually return before this test
	// function does -- t.TempDir()'s own t.Cleanup-registered RemoveAll can
	// otherwise race a publisher write still in flight (WriteFileAtomic's
	// temp-file-then-rename leaves a stray entry RemoveAll trips over:
	// "directory not empty"). A bare `defer cancel()` only asks the
	// goroutine to stop; it doesn't wait for it to have stopped.
	defer func() {
		cancel()
		<-done
	}()

	waitForFileContent(t, path, "bits=0 names=idle")

	release := e.Enter(ToolExec)
	defer release()
	waitForFileContent(t, path, "names=toolexec")
}

func TestRunFilePublisher_EmptyPathIsNoOp(t *testing.T) {
	e := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RunFilePublisher(ctx, e, "")
		close(done)
	}()

	select {
	case <-done:
		// expected: returns immediately, leaving no goroutine (and no
		// file/subscription) behind -- AC-017-7's "no file created" case.
	case <-time.After(time.Second):
		t.Fatalf("RunFilePublisher with an empty path did not return immediately")
	}
}

func waitForFileContent(t *testing.T, path, substr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), substr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state file at %s never contained %q", path, substr)
}
