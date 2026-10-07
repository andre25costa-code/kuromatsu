// Package runstate implements the process-wide "motor de estados" (ADR-016,
// S09): a bitmask with per-bit refcounting that lets heartbeat, cron,
// evolution, the modo dormir bridge, and the process's own SO integration
// (systemd, /health, sysmon) all read and gate on one shared notion of "what
// is the agent doing right now."
//
// This is deliberately not a single-state FSM: several bits can be active
// at once (e.g. Inference + ToolExec during a tool call inside a turn).
// Every bit is refcounted (Inc/Dec) so concurrent holders of the same bit
// don't clear it out from under each other -- see Engine.
package runstate

import (
	"sort"
	"strings"
)

// Mode is a bitmask of runstate bits. The zero value is Idle.
type Mode uint32

// Idle is the zero value: no other bit is active. Declared outside the
// iota block below so Inference lands on bit 0 (1<<0), matching the S09
// bit table exactly.
const Idle Mode = 0

const (
	// Inference is set while a call to the model is decoding (user turn,
	// heartbeat, or cron "Message" job). Always preempts Dream (S09).
	Inference Mode = 1 << iota

	// ToolExec is set while a tool call is executing (top of ExecuteTools).
	ToolExec

	// Dream is set while evolution's cold path or the modo dormir bridge
	// occupy the "inconsciente". Only entered via TryEnterDream, and only
	// when the engine is otherwise Idle.
	Dream

	// Suspended is set by the memory guard (ADR-017) under sustained PSI
	// pressure. While active, no new Inference/ToolExec/Dream entry is
	// accepted -- callers get ErrBusy.
	Suspended

	// Reflex is set while a deterministic reflex (S16/FR-013) is executing.
	Reflex

	// Throttled is purely informative: the CPU-credit watchdog sets it when
	// e2-micro burst credits are exhausted (S06/R7) and clears it once the
	// steal ratio recovers. It never gates any transition -- see S09/S17.
	Throttled
)

// bitNames lists every non-Idle bit in a fixed, low-to-high order, paired
// with its wire/log name. Names/String/Has iterate this instead of the raw
// iota sequence so adding a bit later can't silently reorder existing names.
var bitNames = []struct {
	bit  Mode
	name string
}{
	{Inference, "inference"},
	{ToolExec, "toolexec"},
	{Dream, "dream"},
	{Suspended, "suspended"},
	{Reflex, "reflex"},
	{Throttled, "throttled"},
}

// Has reports whether every bit set in want is also set in m.
func (m Mode) Has(want Mode) bool {
	return m&want == want
}

// Any reports whether at least one bit set in want is also set in m.
func (m Mode) Any(want Mode) bool {
	return m&want != 0
}

// Names returns the sorted (by bit value) list of active bit names. An Idle
// mode returns an empty (non-nil) slice.
func (m Mode) Names() []string {
	names := make([]string, 0, len(bitNames))
	for _, bn := range bitNames {
		if m.Has(bn.bit) {
			names = append(names, bn.name)
		}
	}
	sort.Strings(names)
	return names
}

// String renders m as a comma-separated list of active bit names, or "idle"
// when m is Idle. Used for STATUS=<names> (sd_notify), run/state, and logs.
func (m Mode) String() string {
	if m == Idle {
		return "idle"
	}
	return strings.Join(m.Names(), ",")
}
