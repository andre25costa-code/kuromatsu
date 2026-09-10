// Package sleep implements the optional nightly "modo dormir" memory
// consolidation pass (FR-010, ADR-006): collect recent session digests,
// ask an LLM to fold them into the workspace's MEMORY.md, and write an
// audit report. Disabled by default (BR-005); never runs concurrently
// with an active agent turn (BR-001, enforced by the caller); respects a
// hard per-run token budget (BR-006).
package sleep

// Config mirrors the future config.SleepConfig JSON block. It is defined
// here (rather than imported from pkg/config) so this package stays
// self-contained and testable without pulling in the config package; the
// bridge that maps config.SleepConfig -> sleep.Config lives with the
// scheduler wiring (pkg/agent/sleep_bridge.go).
type Config struct {
	Enabled          bool
	Window           string // "HH:MM-HH:MM", e.g. "03:00-05:00"
	UnconsciousModel string // model_list ref; empty means the agent's default chain
	WeeklyDeep       bool
	MaxTokensBudget  int
	DryRun           bool
}

// WithDefaults fills in the zero-value MaxTokensBudget (BR-006).
func (c Config) WithDefaults() Config {
	if c.MaxTokensBudget <= 0 {
		c.MaxTokensBudget = 20000
	}
	return c
}
