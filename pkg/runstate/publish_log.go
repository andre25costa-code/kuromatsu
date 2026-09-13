package runstate

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// RunLogPublisher logs (info level, component "runstate") the current state
// once and every subsequent transition thereafter, until ctx is done.
// Started only when runstate.enabled=true (gateway.go, C2) -- with the
// feature off this is never called, so no goroutine/log line exists
// (AC-017-7).
func RunLogPublisher(ctx context.Context, e *Engine) {
	if e == nil {
		return
	}
	ch, cancel := e.Subscribe()
	defer cancel()

	first := true
	for {
		select {
		case <-ctx.Done():
			return
		case mode, ok := <-ch:
			if !ok {
				return
			}
			if first {
				logger.InfoCF("runstate", "initial state", map[string]any{"state": mode.String()})
				first = false
				continue
			}
			logger.InfoCF("runstate", "state transition", map[string]any{"state": mode.String()})
		}
	}
}
