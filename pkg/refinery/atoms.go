package refinery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
)

// Atom flags: one uint32 bitmask per atom (ADR-020 §2), so retrieval
// filters become "flags & mask" with no join tables.
const (
	FlagRule       uint32 = 1 << 0
	FlagPreference uint32 = 1 << 1

	FlagPinned       uint32 = 1 << 4
	FlagSensitive    uint32 = 1 << 6 // holds a credential; kept, masked only on the way out (FR-029)
	FlagUserAuthored uint32 = 1 << 7 // written by a person, not by consolidation

	FlagOriginUser      uint32 = 1 << 8
	FlagOriginAssistant uint32 = 1 << 9
	FlagOriginTool      uint32 = 1 << 10
	FlagOriginImport    uint32 = 1 << 11
	FlagOriginSleep     uint32 = 1 << 12
)

// DefaultDedupeHamming is the SimHash distance at or below which a new
// atom is absorbed into an existing one. It is 0 on purpose: after
// normalization (NFC, case, punctuation, whitespace) a true repetition
// fingerprints identically, while a changed value can land very close --
// "às sete" vs "às oito" measured at Hamming 2 (2026-10-06), and the earlier
// k=3 only held by accident of the entity text. Merging two different facts
// silently drops the newer one; a missed repetition only costs one extra
// atom. Lexical similarity is not logical equivalence (context-refinery).
const DefaultDedupeHamming = 0

// Category patterns from context-refinery's exporters.py, plus English.
var (
	rulePattern       = regexp.MustCompile(`(?i)\b(deve|não deve|sempre|nunca|proibid[oa]|exigid[oa]|obrigatóri[oa]|regra|always|never|must|rule)\b`)
	preferencePattern = regexp.MustCompile(`(?i)\b(prefere|preferência|gosta|odeia|evita|quer|deseja|prefers?|likes?|hates?|avoids?)\b`)
	listMarkerPattern = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)
	entityPattern     = regexp.MustCompile(`^\[([^\]]+)\]\s+(.+)$`)
)

// Atom is one canonical fact: "[Entity] - Fact" plus its fingerprint,
// flags and timestamp.
type Atom struct {
	ID      int64
	Entity  string
	Fact    string
	SimHash uint64
	Flags   uint32
	TS      time.Time
}

// Store is the atom table (P2: the source of truth that MEMORY.md is
// rendered from). It keeps the active fingerprints in memory -- 8 bytes per
// atom -- for the linear dedupe scan.
type Store struct {
	db            *sql.DB
	DedupeHamming int
	mu            sync.Mutex
	activeIDs     []int64
	activeFPs     []uint64
	// dataVersion is SQLite's PRAGMA data_version as of the last load: it
	// changes only when another connection (e.g. the FR-026 subprocess)
	// commits, which is when the in-memory index must be reloaded.
	dataVersion int64
}

// OpenStore opens (creating if needed) the atom store at path.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		`CREATE TABLE IF NOT EXISTS atoms (
			id INTEGER PRIMARY KEY,
			entity TEXT NOT NULL,
			fact TEXT NOT NULL,
			simhash INTEGER NOT NULL,
			flags INTEGER NOT NULL,
			ts INTEGER NOT NULL,
			superseded_at INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("refinery: init %s: %w", path, err)
		}
	}
	s := &Store{db: db, DedupeHamming: DefaultDedupeHamming}
	if err := s.loadFingerprints(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// refreshIfChangedElsewhere reloads the fingerprint index when another
// connection committed since the last load. Caller holds s.mu.
func (s *Store) refreshIfChangedElsewhere(ctx context.Context) error {
	v, err := s.readDataVersion(ctx)
	if err != nil || v == s.dataVersion {
		return err
	}
	return s.loadFingerprints()
}

func (s *Store) readDataVersion(ctx context.Context) (int64, error) {
	var v int64
	err := s.db.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v)
	return v, err
}

func (s *Store) loadFingerprints() error {
	s.activeIDs, s.activeFPs = s.activeIDs[:0], s.activeFPs[:0]
	if v, err := s.readDataVersion(context.Background()); err == nil {
		s.dataVersion = v
	}
	rows, err := s.db.Query(`SELECT id, simhash FROM atoms WHERE superseded_at IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, fp int64
		if err := rows.Scan(&id, &fp); err != nil {
			return err
		}
		s.activeIDs = append(s.activeIDs, id)
		s.activeFPs = append(s.activeFPs, uint64(fp))
	}
	return rows.Err()
}

// Add cleans and classifies a fact and stores it, unless a near duplicate
// (Hamming <= DedupeHamming) is already active -- then that atom's ID is
// returned with absorbed=true and nothing is written.
func (s *Store) Add(ctx context.Context, entity, fact string, flags uint32) (id int64, absorbed bool, err error) {
	// An atom is one line: collapsing whitespace (line breaks included)
	// keeps render -> sync from splitting a fact apart.
	entity = strings.Join(strings.Fields(Clean(entity)), " ")
	fact = strings.Join(strings.Fields(Clean(fact)), " ")
	if fact == "" {
		return 0, false, fmt.Errorf("refinery: empty fact")
	}
	flags |= classify(fact)
	fp := SimHash64(entity + ": " + fact)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshIfChangedElsewhere(ctx); err != nil {
		return 0, false, err
	}
	if i, d := Nearest(s.activeFPs, fp); i >= 0 && d <= s.DedupeHamming {
		return s.activeIDs[i], true, nil
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO atoms (entity, fact, simhash, flags, ts) VALUES (?, ?, ?, ?, ?)`,
		entity, fact, int64(fp), flags, time.Now().Unix())
	if err != nil {
		return 0, false, err
	}
	if id, err = res.LastInsertId(); err != nil {
		return 0, false, err
	}
	if s.dataVersion, err = s.readDataVersion(ctx); err != nil {
		return 0, false, err
	}
	s.activeIDs = append(s.activeIDs, id)
	s.activeFPs = append(s.activeFPs, fp)
	return id, false, nil
}

func classify(fact string) uint32 {
	var flags uint32
	if rulePattern.MatchString(fact) {
		flags |= FlagRule
	}
	if preferencePattern.MatchString(fact) {
		flags |= FlagPreference
	}
	if ContainsCredential(fact) {
		flags |= FlagSensitive
	}
	return flags
}

// Supersede retires an atom: it leaves the active set and the dedupe index
// but stays in the table for provenance (nothing is physically deleted).
func (s *Store) Supersede(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE atoms SET superseded_at = ? WHERE id = ? AND superseded_at IS NULL`, time.Now().Unix(), id); err != nil {
		return err
	}
	for i, activeID := range s.activeIDs {
		if activeID == id {
			s.activeIDs = append(s.activeIDs[:i], s.activeIDs[i+1:]...)
			s.activeFPs = append(s.activeFPs[:i], s.activeFPs[i+1:]...)
			break
		}
	}
	return nil
}

// Active returns the non-superseded atoms in insertion order.
func (s *Store) Active(ctx context.Context) ([]Atom, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, entity, fact, simhash, flags, ts FROM atoms WHERE superseded_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
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

// RenderMarkdown renders MEMORY.md from the atoms (P2): rules and
// preferences first, then the other facts, each group in insertion order.
// Deterministic, so an unchanged atom set renders byte-identical.
func RenderMarkdown(atoms []Atom) string {
	sorted := append([]Atom(nil), atoms...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var rules, facts []string
	for _, a := range sorted {
		line := atomLine(a.Entity, a.Fact)
		if a.Flags&(FlagRule|FlagPreference|FlagPinned) != 0 {
			rules = append(rules, line)
		} else {
			facts = append(facts, line)
		}
	}

	var b strings.Builder
	b.WriteString("# Memória\n\n_(Gerado a partir dos átomos da memória; edições manuais neste arquivo são importadas automaticamente.)_\n")
	for _, section := range []struct {
		title string
		lines []string
	}{{"Regras e preferências", rules}, {"Fatos", facts}} {
		if len(section.lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", section.title, strings.Join(section.lines, "\n"))
	}
	return b.String()
}

func atomLine(entity, fact string) string {
	if entity == "" {
		return "- " + fact
	}
	return fmt.Sprintf("- [%s] %s", entity, fact)
}

// WriteMemoryFile renders the active atoms to path and remembers the hash
// of what it wrote, so SyncMemoryFile can tell manual edits apart.
func (s *Store) WriteMemoryFile(ctx context.Context, path string) error {
	atoms, err := s.Active(ctx)
	if err != nil {
		return err
	}
	content := RenderMarkdown(atoms)
	if err := fileutil.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
		return err
	}
	return s.setMeta(ctx, "rendered_hash", contentHash(content))
}

// SyncMemoryFile imports manual edits from MEMORY.md before the next render
// (AC-022-7): lines removed from the file supersede their atoms, new lines
// become user-authored atoms. An untouched file (same hash as the last
// render or sync) is a no-op.
func (s *Store) SyncMemoryFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)
	hash := contentHash(content)
	if last, _ := s.getMeta(ctx, "rendered_hash"); last == hash {
		return nil
	}

	inFile := map[string]bool{}
	var added [][2]string
	for _, raw := range strings.Split(content, "\n") {
		entity, fact, ok := parseAtomLine(strings.TrimSpace(raw))
		if !ok {
			continue
		}
		key := atomLine(entity, fact)
		if !inFile[key] {
			inFile[key] = true
			added = append(added, [2]string{entity, fact})
		}
	}

	atoms, err := s.Active(ctx)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	// Deletions first, so an edited line (old removed, new added) is not
	// absorbed back into the atom it replaces.
	for _, a := range atoms {
		key := atomLine(a.Entity, a.Fact)
		known[key] = true
		if !inFile[key] {
			if err := s.Supersede(ctx, a.ID); err != nil {
				return err
			}
		}
	}
	for _, ef := range added {
		if known[atomLine(ef[0], ef[1])] {
			continue
		}
		if _, _, err := s.Add(ctx, ef[0], ef[1], FlagUserAuthored|FlagOriginImport); err != nil {
			return err
		}
	}
	return s.setMeta(ctx, "rendered_hash", hash)
}

// parseAtomLine reads one MEMORY.md line as an atom. Every content line
// counts -- rendered "- [Entity] fact" items, other list styles and plain
// paragraphs alike -- because the agent writes the file freely with
// write_file: ignoring a line would drop it on the next render. Only
// headings, blank lines and the generated notice are skipped.
func parseAtomLine(line string) (entity, fact string, ok bool) {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "_(Gerado a partir dos átomos") {
		return "", "", false
	}
	line = strings.TrimSpace(listMarkerPattern.ReplaceAllString(line, ""))
	if m := entityPattern.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), true
	}
	return "", line, line != ""
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (s *Store) setMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) getMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}
