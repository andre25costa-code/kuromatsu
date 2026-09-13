package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestRecorder(t *testing.T) (*Recorder, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "turns.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec := NewRecorder(store)
	t.Cleanup(func() { _ = rec.Close() })
	return rec, store
}

func waitForCount(t *testing.T, store *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		agg, err := store.StatsByWindow(context.Background(), time.Now().Add(-time.Hour), "")
		if err != nil {
			t.Fatalf("StatsByWindow: %v", err)
		}
		total := 0
		for _, a := range agg {
			total += a.Count
		}
		if total >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("store never reached %d rows within the deadline", want)
}

func TestRecorder_RecordWritesAsynchronously(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Record(TurnRecord{Window: "files", PromptTokens: 10, TotalMs: 100})
	waitForCount(t, store, 1)
}

func TestRecorder_NilReceiverIsSafe(t *testing.T) {
	var rec *Recorder
	rec.Record(TurnRecord{Window: "files"}) // must not panic
	if got := rec.Dropped(); got != 0 {
		t.Fatalf("nil Recorder.Dropped() = %d, want 0", got)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("nil Recorder.Close() = %v, want nil", err)
	}
}

func TestRecorder_NeverBlocksWhenChannelIsFull(t *testing.T) {
	// Constructed directly (not via NewRecorder) with no drain goroutine
	// running at all, so every call past the channel's capacity (4) is
	// guaranteed to hit the "channel full" drop path deterministically
	// instead of racing a real background writer.
	path := filepath.Join(t.TempDir(), "turns.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	rec := &Recorder{store: store, ch: make(chan TurnRecord, 4)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			rec.Record(TurnRecord{Window: "files"})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Record blocked instead of dropping once the channel filled up (AC-019-2)")
	}

	if got := rec.Dropped(); got == 0 {
		t.Fatal("Dropped() = 0, want > 0 after writing far more records than the channel holds with nothing draining it")
	}
}

func TestRecorder_Close_StopsGoroutineAndClosesStore(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Record(TurnRecord{Window: "files"})
	waitForCount(t, store, 1)

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The store's db is closed now; a further op through it must error
	// rather than silently succeed.
	if err := store.Insert(context.Background(), TurnRecord{}); err == nil {
		t.Fatal("Insert after Close succeeded, want an error (db should be closed)")
	}
}
