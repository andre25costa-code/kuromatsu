package telemetry

import "database/sql"

// runSchema creates the turns table and its indexes. Idempotent (safe to
// run on every Open, mirroring pkg/seahorse's runSchema convention) --
// there is exactly one schema version so far, no ALTER-based migrations
// needed yet.
func runSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS turns (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			ts                 TEXT NOT NULL,
			origin             TEXT NOT NULL DEFAULT '',
			session_key        TEXT NOT NULL DEFAULT '',
			window             TEXT NOT NULL DEFAULT '',
			escalations        INTEGER NOT NULL DEFAULT 0,
			unknown_tool_calls INTEGER NOT NULL DEFAULT 0,
			prompt_tokens      INTEGER NOT NULL DEFAULT 0,
			cached_tokens      INTEGER NOT NULL DEFAULT 0,
			output_tokens      INTEGER NOT NULL DEFAULT 0,
			prefill_ms         INTEGER NOT NULL DEFAULT 0,
			gen_ms             INTEGER NOT NULL DEFAULT 0,
			total_ms           INTEGER NOT NULL DEFAULT 0,
			tools_called       INTEGER NOT NULL DEFAULT 0,
			iterations         INTEGER NOT NULL DEFAULT 0,
			status             TEXT NOT NULL DEFAULT '',
			mode_bits          INTEGER NOT NULL DEFAULT 0,
			rss_kb             INTEGER NOT NULL DEFAULT 0,
			steal_pct          REAL NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_turns_ts ON turns(ts)`,
		`CREATE INDEX IF NOT EXISTS idx_turns_window_ts ON turns(window, ts)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
