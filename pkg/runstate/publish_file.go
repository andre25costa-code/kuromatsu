package runstate

import (
	"context"
	"fmt"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// RunFilePublisher atomically (re)writes path on the current state and
// every subsequent transition (S34), in the format:
//
//	bits=<uint32> names=<comma-separated, sorted, or "idle"> since=<RFC3339>
//
// Runs until ctx is done. Started only when runstate.enabled=true and a
// non-empty path is configured (gateway.go, C2) -- with the feature off
// this is never called, so no file/goroutine exists (AC-017-7).
func RunFilePublisher(ctx context.Context, e *Engine, path string) {
	if e == nil || path == "" {
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
			line := FormatStateFile(mode, e.Since())
			if err := fileutil.WriteFileAtomic(path, []byte(line), 0o644); err != nil {
				logger.WarnCF("runstate", "failed to write state file", map[string]any{
					"path":  path,
					"error": err.Error(),
				})
			}
		}
	}
}

// FormatStateFile renders the run/state file contents for mode/since.
// Exported so tests (and, if ever useful, `sysmon state`) can render/parse
// the same format the publisher writes without re-deriving it.
func FormatStateFile(mode Mode, since time.Time) string {
	return fmt.Sprintf("bits=%d names=%s since=%s\n", uint32(mode), mode.String(), since.Format(time.RFC3339))
}
