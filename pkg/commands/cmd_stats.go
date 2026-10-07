package commands

import (
	"context"
	"strconv"
	"strings"
)

// statsCommand implements /stats [window] [hours] (ADR-017/FR-019, AC-019-4):
// aggregates the per-turn telemetry Trilho C records (count, average
// prompt tokens, % served from KV cache, average output tokens, average
// and p50/p90 total latency) grouped by focus window and by hour of day.
func statsCommand() Definition {
	return Definition{
		Name:        "stats",
		Description: "Show turn telemetry stats (by focus window and hour of day)",
		Usage:       "/stats [window] [hours]",
		Handler: func(ctx context.Context, req Request, rt *Runtime) error {
			if rt == nil || rt.QueryStats == nil {
				return req.Reply(unavailableMsg)
			}
			window, hours := parseStatsArgs(req.Text)
			out, err := rt.QueryStats(ctx, window, hours)
			if err != nil {
				return req.Reply(err.Error())
			}
			return req.Reply(out)
		},
	}
}

// parseStatsArgs parses the free-form text after "/stats". Both arguments
// are optional and either order-independent between "window" and "hours"
// is resolved by shape, not position: whichever token parses as a
// positive integer is the hours argument; the other (if any) is the
// window name. "" for window means "every window"; 0 for hours means "use
// the caller's own default" (queryStats' defaultStatsWindowHours).
func parseStatsArgs(text string) (window string, hours int) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", 0
	}
	// fields[0] is the command token itself ("/stats" or "!stats"); the
	// actual arguments start at index 1.
	args := fields[1:]

	for _, arg := range args {
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			hours = n
			continue
		}
		if window == "" {
			window = arg
		}
	}
	return window, hours
}
