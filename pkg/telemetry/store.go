// Package telemetry implements the Trilho C per-turn telemetry store
// (ADR-017/FR-019, S34): one SQLite row per completed turn (including
// reflexes, AC-019-3) in $KUROMATSU_HOME/telemetry/turns.db, using
// modernc.org/sqlite -- the same pure-Go driver pkg/seahorse already uses
// (ADR-008), so no new cgo dependency. Store is the synchronous storage
// layer (open/insert/prune/aggregate); Recorder (recorder.go) is the
// async, never-blocks-the-turn wrapper telemetry_bridge.go actually calls.
package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteTimeLayout mirrors pkg/seahorse's own layout (UTC, second
// precision, lexically sortable) -- kept identical so anyone inspecting
// both databases with sqlite3 directly sees the same convention.
const sqliteTimeLayout = "2006-01-02 15:04:05"

// TurnRecord is one row of the turns table (AC-019-1's exact column list).
type TurnRecord struct {
	// Ts is the turn's completion time. Zero means "use time.Now()" --
	// Insert fills it in so callers never have to.
	Ts               time.Time
	Origin           string
	SessionKey       string
	Window           string
	Escalations      int
	UnknownToolCalls int
	PromptTokens     int
	CachedTokens     int
	OutputTokens     int
	PrefillMs        int64
	GenMs            int64
	TotalMs          int64
	ToolsCalled      int
	Iterations       int
	Status           string
	ModeBits         uint32
	RSSKb            int64
	StealPct         float64
}

// Store is the SQLite-backed turns.db (WAL mode, busy_timeout, NORMAL
// synchronous -- the same pragmas pkg/seahorse's NewEngine applies, for
// the same reason: safe concurrent access from the Recorder's single
// writer goroutine plus /stats reads without SQLITE_BUSY under normal
// load).
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the turns.db at dbPath and ensures its
// schema. dbPath must be non-empty -- callers gate that via
// config.TelemetryConfig.EffectiveDBPath (empty means "telemetry
// disabled", AC-019-6), never by passing "" here.
func Open(dbPath string) (*Store, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("telemetry: empty db path")
	}
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("telemetry: create db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("telemetry: open db: %w", err)
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("telemetry: apply pragma %q: %w", pragma, err)
		}
	}

	if err := runSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("telemetry: schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database handle. Safe on a nil *Store.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Insert writes one turn row. rec.Ts defaults to time.Now() when zero.
func (s *Store) Insert(ctx context.Context, rec TurnRecord) error {
	ts := rec.Ts
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO turns (
			ts, origin, session_key, window, escalations, unknown_tool_calls,
			prompt_tokens, cached_tokens, output_tokens, prefill_ms, gen_ms,
			total_ms, tools_called, iterations, status, mode_bits, rss_kb, steal_pct
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatSQLiteTime(ts), rec.Origin, rec.SessionKey, rec.Window,
		rec.Escalations, rec.UnknownToolCalls,
		rec.PromptTokens, rec.CachedTokens, rec.OutputTokens,
		rec.PrefillMs, rec.GenMs, rec.TotalMs,
		rec.ToolsCalled, rec.Iterations, rec.Status, rec.ModeBits, rec.RSSKb, rec.StealPct,
	)
	if err != nil {
		return fmt.Errorf("telemetry: insert turn: %w", err)
	}
	return nil
}

// Prune deletes every row older than retentionDays (AC-019-5). Returns
// the number of rows removed.
func (s *Store) Prune(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	res, err := s.db.ExecContext(ctx, "DELETE FROM turns WHERE ts < ?", formatSQLiteTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("telemetry: prune: %w", err)
	}
	return res.RowsAffected()
}

// turnRow is one raw row read back for aggregation -- just the columns
// StatsByWindow/StatsByHourOfDay's Go-side percentile math needs, plus
// whatever grouping expression the caller selected as "label".
type turnRow struct {
	Label        string
	PromptTokens int
	CachedTokens int
	OutputTokens int
	TotalMs      int64
}

// fetchGrouped runs one filtered SELECT (SQL does the WHERE/grouping-key
// projection and ORDER BY; percentiles are computed in Go from the
// resulting per-group slices -- see Aggregate/percentile) and returns rows
// bucketed by groupExpr's value, plus the bucket labels in first-seen
// (i.e. SQL ORDER BY) order so callers can render deterministically.
func (s *Store) fetchGrouped(ctx context.Context, groupExpr string, since time.Time, windowFilter string) (map[string][]turnRow, []string, error) {
	query := fmt.Sprintf(
		`SELECT %s AS label, prompt_tokens, cached_tokens, output_tokens, total_ms
		 FROM turns WHERE ts >= ?`, groupExpr)
	args := []any{formatSQLiteTime(since)}
	if windowFilter != "" {
		query += " AND window = ?"
		args = append(args, windowFilter)
	}
	query += " ORDER BY label"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry: query: %w", err)
	}
	defer rows.Close()

	groups := map[string][]turnRow{}
	var order []string
	for rows.Next() {
		var r turnRow
		if err := rows.Scan(&r.Label, &r.PromptTokens, &r.CachedTokens, &r.OutputTokens, &r.TotalMs); err != nil {
			return nil, nil, fmt.Errorf("telemetry: scan: %w", err)
		}
		if _, seen := groups[r.Label]; !seen {
			order = append(order, r.Label)
		}
		groups[r.Label] = append(groups[r.Label], r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("telemetry: iterate rows: %w", err)
	}
	return groups, order, nil
}

// Aggregate is one row of a /stats report (AC-019-4): count, prompt/output
// token averages, the % of the prompt that was served from KV cache
// (S21/S34), average total latency, and p50/p90 latency.
type Aggregate struct {
	Label            string
	Count            int
	AvgPromptTokens  float64
	AvgCachedPercent float64
	AvgOutputTokens  float64
	AvgTotalMs       float64
	P50Ms            float64
	P90Ms            float64
}

// StatsByWindow aggregates turns since `since` (optionally filtered to one
// window) grouped by focus window.
func (s *Store) StatsByWindow(ctx context.Context, since time.Time, windowFilter string) ([]Aggregate, error) {
	groups, order, err := s.fetchGrouped(ctx, "window", since, windowFilter)
	if err != nil {
		return nil, err
	}
	return aggregateGroups(groups, order), nil
}

// StatsByHourOfDay aggregates turns since `since` (optionally filtered to
// one window) grouped by the two-digit UTC hour of day ("00".."23") the
// turn completed in.
func (s *Store) StatsByHourOfDay(ctx context.Context, since time.Time, windowFilter string) ([]Aggregate, error) {
	// ts is stored as UTC "YYYY-MM-DD HH:MM:SS"; strftime('%H', ts) reads
	// the UTC hour directly -- deliberately not localtime-adjusted, so the
	// bucket a row lands in never depends on the server's TZ setting at
	// query time.
	groups, order, err := s.fetchGrouped(ctx, "strftime('%H', ts)", since, windowFilter)
	if err != nil {
		return nil, err
	}
	return aggregateGroups(groups, order), nil
}

func aggregateGroups(groups map[string][]turnRow, order []string) []Aggregate {
	out := make([]Aggregate, 0, len(order))
	for _, label := range order {
		rows := groups[label]
		out = append(out, aggregateOne(label, rows))
	}
	return out
}

func aggregateOne(label string, rows []turnRow) Aggregate {
	agg := Aggregate{Label: label, Count: len(rows)}
	if len(rows) == 0 {
		return agg
	}

	var sumPrompt, sumOutput int64
	var sumCachedPct float64
	cachedPctSamples := 0
	totals := make([]int64, len(rows))
	for i, r := range rows {
		sumPrompt += int64(r.PromptTokens)
		sumOutput += int64(r.OutputTokens)
		if r.PromptTokens > 0 {
			sumCachedPct += 100 * float64(r.CachedTokens) / float64(r.PromptTokens)
			cachedPctSamples++
		}
		totals[i] = r.TotalMs
	}
	sort.Slice(totals, func(i, j int) bool { return totals[i] < totals[j] })

	n := float64(len(rows))
	agg.AvgPromptTokens = float64(sumPrompt) / n
	agg.AvgOutputTokens = float64(sumOutput) / n
	if cachedPctSamples > 0 {
		agg.AvgCachedPercent = sumCachedPct / float64(cachedPctSamples)
	}
	var sumTotal int64
	for _, t := range totals {
		sumTotal += t
	}
	agg.AvgTotalMs = float64(sumTotal) / n
	agg.P50Ms = percentile(totals, 0.50)
	agg.P90Ms = percentile(totals, 0.90)
	return agg
}

// percentile uses the nearest-rank method over an already-sorted
// (ascending) slice -- simple and deterministic, which matters more here
// than interpolation accuracy for a handful of turns per window/hour.
func percentile(sorted []int64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return float64(sorted[idx])
}

func formatSQLiteTime(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(sqliteTimeLayout)
}
