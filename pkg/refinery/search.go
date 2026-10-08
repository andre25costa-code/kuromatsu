package refinery

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"
)

// ftsVersion is bumped when the atoms_fts definition changes, so existing
// stores rebuild the index once on open.
const ftsVersion = "1"

// maxQueryTerms caps how many words of a message become FTS terms: the
// query is the user's whole message, and a long one should not turn into a
// huge OR expression.
const maxQueryTerms = 16

// FloorFlags selects the atoms that enter the prompt regardless of the
// query (AC-022-3): pinned atoms and the rule/preference categories.
const FloorFlags = FlagPinned | FlagRule | FlagPreference

// ensureFTSBuilt (re)builds atoms_fts from the atoms table once per
// ftsVersion: stores created before the index existed have atoms the
// triggers never saw.
func ensureFTSBuilt(db *sql.DB) error {
	var have string
	err := db.QueryRow(`SELECT value FROM meta WHERE key = 'fts_version'`).Scan(&have)
	if err == nil && have == ftsVersion {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if _, err = db.Exec(`INSERT INTO atoms_fts(atoms_fts) VALUES ('rebuild')`); err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO meta (key, value) VALUES ('fts_version', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		ftsVersion,
	)
	return err
}

// Search returns up to limit active atoms matching the words of query,
// best BM25 match first. The query is free text (a user message): every
// word is quoted, so FTS5 syntax in it is never interpreted, and words are
// OR-ed, since requiring all of a message's words in one atom would almost
// never match.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Atom, error) {
	match := ftsQuery(query)
	if match == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.entity, a.fact, a.simhash, a.flags, a.ts
		FROM atoms_fts JOIN atoms a ON a.id = atoms_fts.rowid
		WHERE atoms_fts MATCH ? AND a.superseded_at IS NULL
		ORDER BY bm25(atoms_fts)
		LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	return scanAtoms(rows)
}

// Floor returns the active atoms that are always in the prompt (FloorFlags),
// oldest first. It reads only those rows, never the whole table (AC-022-6).
func (s *Store) Floor(ctx context.Context) ([]Atom, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, entity, fact, simhash, flags, ts FROM atoms
		WHERE superseded_at IS NULL AND (flags & ?) != 0
		ORDER BY id`, FloorFlags)
	if err != nil {
		return nil, err
	}
	return scanAtoms(rows)
}

// HasActive reports whether the store holds at least one active atom.
func (s *Store) HasActive(ctx context.Context) (bool, error) {
	var has bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM atoms WHERE superseded_at IS NULL)`).Scan(&has)
	return has, err
}

func scanAtoms(rows *sql.Rows) ([]Atom, error) {
	defer rows.Close()
	var atoms []Atom
	for rows.Next() {
		var a Atom
		var fp, ts int64
		if err := rows.Scan(&a.ID, &a.Entity, &a.Fact, &fp, &a.Flags, &ts); err != nil {
			return nil, err
		}
		a.SimHash, a.TS = uint64(fp), time.Unix(ts, 0)
		atoms = append(atoms, a)
	}
	return atoms, rows.Err()
}

// ftsQuery turns free text into a safe FTS5 expression: the message's
// words (letters and digits), deduplicated, each double-quoted, OR-ed.
func ftsQuery(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool, len(words))
	terms := make([]string, 0, maxQueryTerms)
	for _, w := range words {
		if seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, `"`+w+`"`)
		if len(terms) == maxQueryTerms {
			break
		}
	}
	return strings.Join(terms, " OR ")
}
