package runstate

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrBusy is returned by TryEnterDream when the engine cannot accept a new
// Dream occupant (something else is active, or the engine is Suspended). It
// is also the sentinel dreamGate implementations (pkg/agent/
// evolution_bridge.go, sleep_bridge.go) surface up through
// evolution.ColdPathRunner/sleep scheduling so callers can tell "busy, try
// again later" apart from a real failure (S17).
var ErrBusy = errors.New("runstate: busy")

// bitCount is the number of non-Idle bits tracked by Engine.
const bitCount = 6

// Engine is a bitmask of refcounted bits (S09/ADR-016): Inc/Dec (or the
// Enter shortcut) track how many concurrent holders each bit has, and the
// aggregate Snapshot() is the OR of every bit with a non-zero refcount.
//
// Default() returns the single process-wide instance production wiring
// (gateway.go, heartbeat, health, sysmon, evolution/sleep bridges) shares.
// New() returns an independent instance for tests, so exercising Engine
// mechanics never touches -- or is affected by -- global state shared with
// other tests in the same package/binary.
type Engine struct {
	mu     sync.Mutex
	counts [bitCount]int32
	since  time.Time

	// dreamCancel is non-nil only while Dream is held (TryEnterDream sets
	// it, its own release() or a preempting Inc(Inference) clears it).
	// Single field, not a slice/map: TryEnterDream's Idle-only precondition
	// makes it impossible for two Dream holders to be active at once.
	dreamCancel context.CancelFunc

	subs   map[int]chan Mode
	nextID int
}

// New returns a fresh, independent Engine starting at Idle.
func New() *Engine {
	return &Engine{since: time.Now()}
}

var (
	defaultOnce   sync.Once
	defaultEngine *Engine
)

// Default returns the process-wide Engine. Always the same *Engine for the
// life of the process -- callers must never call New() where they mean
// Default(), or they'll end up gating on two disconnected bitmasks.
func Default() *Engine {
	defaultOnce.Do(func() { defaultEngine = New() })
	return defaultEngine
}

// bitIndex maps a single bit constant to its slot in counts/bitNames, or -1
// for anything that isn't exactly one of the known bits (Idle, a
// combination of bits, or garbage).
func bitIndex(bit Mode) int {
	for i, bn := range bitNames {
		if bn.bit == bit {
			return i
		}
	}
	return -1
}

// currentLocked recomputes the aggregate Mode from the refcounts. Caller
// must hold mu.
func (e *Engine) currentLocked() Mode {
	var m Mode
	for i, bn := range bitNames {
		if e.counts[i] > 0 {
			m |= bn.bit
		}
	}
	return m
}

// noteTransitionLocked updates since and notifies subscribers when the
// aggregate mode actually changed. Caller must hold mu.
func (e *Engine) noteTransitionLocked(before, after Mode) {
	if before == after {
		return
	}
	e.since = time.Now()
	for _, ch := range e.subs {
		select {
		case ch <- after:
		default:
			// Size-1 "latest wins" channel: drop the stale pending value
			// (if any) and retry once so a slow subscriber sees the newest
			// Mode instead of getting stuck on an old one. Never blocks.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- after:
			default:
			}
		}
	}
}

// Inc increments bit's refcount. A no-op for anything that isn't exactly
// one known bit. Inference preempts an in-flight Dream (S09): the first
// Inc(Inference) while Dream is held cancels the context TryEnterDream
// handed out, so the dreamer unwinds and its caller reschedules.
//
// Inc never refuses -- it predates Suspended's veto and several call sites
// (Reflex via rsEnter) are deliberately never gated by it. Inference/
// ToolExec call sites that must honor "Suspended vetoes new entries" (S09/
// ADR-016 point 6) use TryEnter instead; see its doc comment.
func (e *Engine) Inc(bit Mode) {
	idx := bitIndex(bit)
	if idx < 0 {
		return
	}
	e.mu.Lock()
	before, after, cancel := e.incLocked(idx, bit)
	e.noteTransitionLocked(before, after)
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// incLocked applies the actual increment + Dream-preemption bookkeeping
// shared by Inc and TryEnter. Caller must hold mu and must call
// noteTransitionLocked(before, after) and, once unlocked, cancel() (if
// non-nil) exactly as Inc does below.
func (e *Engine) incLocked(idx int, bit Mode) (before, after Mode, cancel context.CancelFunc) {
	before = e.currentLocked()
	e.counts[idx]++
	after = e.currentLocked()
	if bit == Inference && e.dreamCancel != nil {
		cancel = e.dreamCancel
		e.dreamCancel = nil
	}
	return before, after, cancel
}

// TryEnter is Inc(bit) plus release, refusing when the engine is Suspended
// (S09/ADR-016 point 6: "enquanto Suspended está ativo, nenhuma nova
// entrada de Inference/ToolExec/Dream é aceita -- chamadas recebem
// ErrBusy"). Dream already has its own gate (TryEnterDream, which also
// requires Idle); this is for Inference/ToolExec call sites, which have no
// such Idle requirement -- concurrent Inference+ToolExec, or several
// concurrent holders of the same bit, are normal and must keep working,
// only Suspended vetoes a *new* entry. ok is false (release a safe no-op)
// when Suspended is active; callers should treat that exactly like
// TryEnterDream's ok=false -- ErrBusy, retriable. A bit that isn't exactly
// one known bit always succeeds (mirrors Inc/Enter's own no-op-for-garbage
// behavior; there is no refcount to veto).
func (e *Engine) TryEnter(bit Mode) (release func(), ok bool) {
	idx := bitIndex(bit)
	if idx < 0 {
		return func() {}, true
	}
	e.mu.Lock()
	if e.counts[bitIndex(Suspended)] > 0 {
		e.mu.Unlock()
		return func() {}, false
	}
	before, after, cancel := e.incLocked(idx, bit)
	e.noteTransitionLocked(before, after)
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	var once sync.Once
	return func() {
		once.Do(func() { e.Dec(bit) })
	}, true
}

// Dec decrements bit's refcount (floored at 0 -- an unmatched Dec is a bug
// elsewhere, not a reason to panic or go negative). A no-op for anything
// that isn't exactly one known bit.
func (e *Engine) Dec(bit Mode) {
	idx := bitIndex(bit)
	if idx < 0 {
		return
	}
	e.mu.Lock()
	before := e.currentLocked()
	if e.counts[idx] > 0 {
		e.counts[idx]--
	}
	after := e.currentLocked()
	e.noteTransitionLocked(before, after)
	e.mu.Unlock()
}

// Enter is Inc(bit) plus a release func that Dec(bit)s exactly once, no
// matter how many times release is called. Callers use it as
// `release := e.Enter(bit); defer release()`.
func (e *Engine) Enter(bit Mode) func() {
	e.Inc(bit)
	var once sync.Once
	return func() {
		once.Do(func() { e.Dec(bit) })
	}
}

// Snapshot returns the current aggregate Mode.
func (e *Engine) Snapshot() Mode {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentLocked()
}

// Since returns when the aggregate Mode last changed (i.e. how long the
// engine has been in its current Snapshot()). Exposed so /health and
// run/state can surface it -- a long-lived Inference is a "leaked bit" red
// flag (S09/S34), not a legitimate turn.
func (e *Engine) Since() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.since
}

// Suspend sets the Suspended bit (memguard, ADR-017, under sustained PSI
// pressure). Idempotent: calling it while already suspended is a no-op.
// Suspend/Resume are a simple on/off pair (not refcounted like the other
// bits) because there is exactly one caller (the memguard PSI watchdog),
// which itself only ever calls one or the other based on a threshold
// crossing -- refcounting would just let a bug there wedge Suspended on.
func (e *Engine) Suspend() {
	e.mu.Lock()
	before := e.currentLocked()
	e.counts[bitIndex(Suspended)] = 1
	after := e.currentLocked()
	e.noteTransitionLocked(before, after)
	e.mu.Unlock()
}

// Resume clears the Suspended bit. Idempotent.
func (e *Engine) Resume() {
	e.mu.Lock()
	before := e.currentLocked()
	e.counts[bitIndex(Suspended)] = 0
	after := e.currentLocked()
	e.noteTransitionLocked(before, after)
	e.mu.Unlock()
}

// TryEnterDream attempts to enter Dream (evolution's cold path, or the modo
// dormir bridge). It only succeeds when the engine is currently exactly
// Idle (S09: no other bit, Dream included, may be active) and not
// Suspended -- Idle already implies !Suspended since Suspended is itself a
// bit. On success, ctx is derived from parent and is canceled the instant
// any Inference begins (preemption) or release is called (whichever comes
// first); release must be called exactly once when the dream work is done,
// win or lose. ok is false (ctx nil, release a safe no-op) when the engine
// is busy -- callers should treat that the same as ErrBusy and retry later
// (evolution: ~2min; sleep: until the window's deadline, S17).
func (e *Engine) TryEnterDream(parent context.Context) (ctx context.Context, release func(), ok bool) {
	e.mu.Lock()
	if e.currentLocked() != Idle {
		e.mu.Unlock()
		return nil, func() {}, false
	}

	before := Idle
	e.counts[bitIndex(Dream)] = 1
	after := e.currentLocked()
	dreamCtx, cancel := context.WithCancel(parent)
	e.dreamCancel = cancel
	e.noteTransitionLocked(before, after)
	e.mu.Unlock()

	var once sync.Once
	release = func() {
		once.Do(func() {
			e.mu.Lock()
			if e.dreamCancel != nil {
				e.dreamCancel()
				e.dreamCancel = nil
			}
			before := e.currentLocked()
			e.counts[bitIndex(Dream)] = 0
			after := e.currentLocked()
			e.noteTransitionLocked(before, after)
			e.mu.Unlock()
		})
	}
	return dreamCtx, release, true
}

// Subscribe returns a channel that receives the current Mode immediately
// and every subsequent transition thereafter (size-1, "latest wins" -- a
// slow consumer sees the newest Mode, never a growing backlog of stale
// ones). cancel unregisters the subscription; safe to call more than once.
// Used by the publish_*.go goroutines, never by hook call sites.
func (e *Engine) Subscribe() (<-chan Mode, func()) {
	e.mu.Lock()
	if e.subs == nil {
		e.subs = make(map[int]chan Mode)
	}
	id := e.nextID
	e.nextID++
	ch := make(chan Mode, 1)
	ch <- e.currentLocked()
	e.subs[id] = ch
	e.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			e.mu.Lock()
			delete(e.subs, id)
			e.mu.Unlock()
		})
	}
	return ch, cancel
}
