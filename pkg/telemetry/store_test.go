package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "turns.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustInsert(t *testing.T, store *Store, ctx context.Context, rec TurnRecord) {
	t.Helper()
	if err := store.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert(%+v): %v", rec, err)
	}
}

func TestOpen_EmptyPathErrors(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") = nil error, want an error (AC-019-6: callers must gate on EffectiveDBPath)")
	}
}

func TestOpen_CreatesSchemaIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "turns.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Re-opening the same path (schema already applied) must not error.
	store2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open on the same db: %v", err)
	}
	defer store2.Close()
}

func TestStore_InsertAndStatsByWindow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	rows := []TurnRecord{
		{Ts: now, Window: "files", PromptTokens: 100, CachedTokens: 50, OutputTokens: 20, TotalMs: 1000},
		{Ts: now, Window: "files", PromptTokens: 200, CachedTokens: 100, OutputTokens: 40, TotalMs: 2000},
		{Ts: now, Window: "chat", PromptTokens: 50, CachedTokens: 40, OutputTokens: 10, TotalMs: 500},
	}
	for _, r := range rows {
		if err := store.Insert(ctx, r); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	agg, err := store.StatsByWindow(ctx, now.Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("StatsByWindow: %v", err)
	}
	if len(agg) != 2 {
		t.Fatalf("StatsByWindow returned %d groups, want 2 (files, chat)", len(agg))
	}

	byLabel := map[string]Aggregate{}
	for _, a := range agg {
		byLabel[a.Label] = a
	}

	files, ok := byLabel["files"]
	if !ok {
		t.Fatal("no \"files\" group in StatsByWindow result")
	}
	if files.Count != 2 {
		t.Fatalf("files.Count = %d, want 2", files.Count)
	}
	if files.AvgPromptTokens != 150 {
		t.Fatalf("files.AvgPromptTokens = %v, want 150", files.AvgPromptTokens)
	}
	// cached% per row: 50/100*100=50, 100/200*100=50 -> avg 50
	if files.AvgCachedPercent != 50 {
		t.Fatalf("files.AvgCachedPercent = %v, want 50", files.AvgCachedPercent)
	}
	if files.AvgTotalMs != 1500 {
		t.Fatalf("files.AvgTotalMs = %v, want 1500", files.AvgTotalMs)
	}

	chat, ok := byLabel["chat"]
	if !ok {
		t.Fatal("no \"chat\" group in StatsByWindow result")
	}
	if chat.Count != 1 {
		t.Fatalf("chat.Count = %d, want 1", chat.Count)
	}
}

func TestStore_StatsByWindow_FilterToOneWindow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	mustInsert(t, store, ctx, TurnRecord{Ts: now, Window: "files", PromptTokens: 10, TotalMs: 100})
	mustInsert(t, store, ctx, TurnRecord{Ts: now, Window: "chat", PromptTokens: 20, TotalMs: 200})

	agg, err := store.StatsByWindow(ctx, now.Add(-time.Hour), "files")
	if err != nil {
		t.Fatalf("StatsByWindow: %v", err)
	}
	if len(agg) != 1 || agg[0].Label != "files" {
		t.Fatalf("StatsByWindow(filter=files) = %+v, want exactly one \"files\" group", agg)
	}
}

func TestStore_StatsByWindow_SinceExcludesOlderRows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	mustInsert(t, store, ctx, TurnRecord{Ts: now.Add(-48 * time.Hour), Window: "files", PromptTokens: 10, TotalMs: 100})
	mustInsert(t, store, ctx, TurnRecord{Ts: now, Window: "files", PromptTokens: 20, TotalMs: 200})

	agg, err := store.StatsByWindow(ctx, now.Add(-24*time.Hour), "")
	if err != nil {
		t.Fatalf("StatsByWindow: %v", err)
	}
	if len(agg) != 1 {
		t.Fatalf("StatsByWindow(since=24h) groups = %d, want 1", len(agg))
	}
	if agg[0].Count != 1 {
		t.Fatalf("StatsByWindow(since=24h) count = %d, want 1 (the 48h-old row must be excluded)", agg[0].Count)
	}
}

func TestStore_StatsByHourOfDay_GroupsByUTCHour(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 12, 9, 30, 0, 0, time.UTC)

	mustInsert(t, store, ctx, TurnRecord{Ts: base, Window: "files", PromptTokens: 10, TotalMs: 100})
	mustInsert(t, store, ctx, TurnRecord{Ts: base.Add(20 * time.Minute), Window: "files", PromptTokens: 20, TotalMs: 200})
	mustInsert(t, store, ctx, TurnRecord{Ts: base.Add(time.Hour), Window: "files", PromptTokens: 30, TotalMs: 300})

	agg, err := store.StatsByHourOfDay(ctx, base.Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("StatsByHourOfDay: %v", err)
	}
	byLabel := map[string]Aggregate{}
	for _, a := range agg {
		byLabel[a.Label] = a
	}
	if got := byLabel["09"].Count; got != 2 {
		t.Fatalf("hour 09 count = %d, want 2", got)
	}
	if got := byLabel["10"].Count; got != 1 {
		t.Fatalf("hour 10 count = %d, want 1", got)
	}
}

func TestStore_Percentiles(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	// 10 rows with total_ms 100..1000 in steps of 100.
	for i := 1; i <= 10; i++ {
		mustInsert(t, store, ctx, TurnRecord{Ts: now, Window: "files", TotalMs: int64(i * 100)})
	}

	agg, err := store.StatsByWindow(ctx, now.Add(-time.Hour), "files")
	if err != nil {
		t.Fatalf("StatsByWindow: %v", err)
	}
	if len(agg) != 1 {
		t.Fatalf("groups = %d, want 1", len(agg))
	}
	a := agg[0]
	if a.Count != 10 {
		t.Fatalf("Count = %d, want 10", a.Count)
	}
	// nearest-rank over sorted [100..1000]: p50 idx=int(0.5*9)=4 -> 500; p90 idx=int(0.9*9)=8 -> 900.
	if a.P50Ms != 500 {
		t.Fatalf("P50Ms = %v, want 500", a.P50Ms)
	}
	if a.P90Ms != 900 {
		t.Fatalf("P90Ms = %v, want 900", a.P90Ms)
	}
}

func TestStore_ReflexRow_ZeroPromptExcludedFromCachedPercentAverage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	// AC-019-3: a reflex turn records prompt_tokens=0/output_tokens=0.
	mustInsert(t, store, ctx, TurnRecord{Ts: now, Window: "", PromptTokens: 0, OutputTokens: 0, TotalMs: 5})
	mustInsert(t, store, ctx, TurnRecord{Ts: now, Window: "", PromptTokens: 100, CachedTokens: 80, OutputTokens: 10, TotalMs: 500})

	agg, err := store.StatsByWindow(ctx, now.Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("StatsByWindow: %v", err)
	}
	if len(agg) != 1 {
		t.Fatalf("groups = %d, want 1 (both rows share window=\"\")", len(agg))
	}
	a := agg[0]
	if a.Count != 2 {
		t.Fatalf("Count = %d, want 2", a.Count)
	}
	// Only the second row contributes a cached% sample (80/100*100=80);
	// the reflex row (prompt_tokens=0) must not pull that average toward 0.
	if a.AvgCachedPercent != 80 {
		t.Fatalf("AvgCachedPercent = %v, want 80 (reflex row must not skew the average)", a.AvgCachedPercent)
	}
}

func TestStore_Prune_RemovesOnlyOlderRows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	mustInsert(t, store, ctx, TurnRecord{Ts: now.Add(-40 * 24 * time.Hour), Window: "files"})
	mustInsert(t, store, ctx, TurnRecord{Ts: now, Window: "files"})

	removed, err := store.Prune(ctx, 30)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Prune removed %d rows, want 1", removed)
	}

	agg, err := store.StatsByWindow(ctx, now.Add(-100*24*time.Hour), "")
	if err != nil {
		t.Fatalf("StatsByWindow: %v", err)
	}
	if len(agg) != 1 || agg[0].Count != 1 {
		t.Fatalf("StatsByWindow after Prune = %+v, want exactly one remaining row", agg)
	}
}

func TestStore_Prune_ZeroRetentionIsNoOp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustInsert(t, store, ctx, TurnRecord{Ts: time.Now().Add(-1000 * 24 * time.Hour), Window: "files"})

	removed, err := store.Prune(ctx, 0)
	if err != nil {
		t.Fatalf("Prune(0): %v", err)
	}
	if removed != 0 {
		t.Fatalf("Prune(0) removed %d rows, want 0 (a non-positive retention must be a no-op, never \"delete everything\")", removed)
	}
}

func TestStore_EmptyStatsReturnsNoGroups(t *testing.T) {
	store := newTestStore(t)
	agg, err := store.StatsByWindow(context.Background(), time.Now().Add(-time.Hour), "")
	if err != nil {
		t.Fatalf("StatsByWindow: %v", err)
	}
	if len(agg) != 0 {
		t.Fatalf("StatsByWindow on an empty store = %+v, want no groups", agg)
	}
}
