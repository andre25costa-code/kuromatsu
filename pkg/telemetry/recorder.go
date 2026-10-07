package telemetry

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// recorderBufferSize is the Recorder's channel capacity (ADR-017/AC-019-2:
// "canal de 256, nunca bloqueia").
const recorderBufferSize = 256

// Recorder is the async, never-blocks-the-turn writer in front of Store:
// Record enqueues onto a size-256 buffered channel and returns immediately;
// a single background goroutine drains it into Store.Insert. When the
// channel is full, the record is dropped and Dropped()'s counter
// increments instead of blocking the caller (AC-019-2) -- a turn must
// never wait on telemetry.
type Recorder struct {
	store   *Store
	ch      chan TurnRecord
	dropped atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRecorder starts the background writer goroutine against store and
// returns the Recorder. store must be non-nil (callers construct it via
// Open first) -- NewRecorder does not open or close it; Recorder.Close
// does.
func NewRecorder(store *Store) *Recorder {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Recorder{
		store:  store,
		ch:     make(chan TurnRecord, recorderBufferSize),
		cancel: cancel,
	}
	r.wg.Add(1)
	go r.run(ctx)
	return r
}

func (r *Recorder) run(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case rec := <-r.ch:
			if err := r.store.Insert(context.Background(), rec); err != nil {
				logger.WarnCF("telemetry", "failed to write turn record", map[string]any{
					"error": err.Error(),
				})
			}
		}
	}
}

// Record enqueues rec for asynchronous insertion. Never blocks: when the
// channel is full it drops rec and increments the dropped counter
// (AC-019-2). Safe on a nil *Recorder (a no-op) so callers never need a
// separate "is telemetry enabled" check at every call site.
func (r *Recorder) Record(rec TurnRecord) {
	if r == nil {
		return
	}
	select {
	case r.ch <- rec:
	default:
		n := r.dropped.Add(1)
		logger.WarnCF("telemetry", "recorder channel full, dropping turn record", map[string]any{
			"dropped_total": n,
		})
	}
}

// Dropped returns how many records have been discarded so far because the
// channel was full (AC-019-2).
func (r *Recorder) Dropped() int64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

// Close stops the background writer and closes the underlying Store.
// Any record still sitting in the channel when Close is called is best-
// effort only -- it may or may not be written before the goroutine exits;
// telemetry is explicitly not meant to add shutdown latency or guarantee
// delivery (S17: best-effort, retry-from-scratch is the norm elsewhere in
// Trilho C too). Safe on a nil *Recorder.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.cancel()
	r.wg.Wait()
	return r.store.Close()
}
