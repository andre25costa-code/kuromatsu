package refinery

import (
	"context"
	"path/filepath"
	"testing"
)

func mustAdd(t *testing.T, s *Store, entity, fact string, flags uint32) int64 {
	t.Helper()
	id, _, err := s.Add(context.Background(), entity, fact, flags)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ids(atoms []Atom) []int64 {
	out := make([]int64, 0, len(atoms))
	for _, a := range atoms {
		out = append(out, a.ID)
	}
	return out
}

func TestSearch_FindsAtomsByMessageTermsAndSkipsSuperseded(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	backup := mustAdd(t, s, "VM", "o backup roda às 3h no disco externo", FlagOriginUser)
	old := mustAdd(t, s, "VM", "o backup antigo ia para o pendrive", FlagOriginUser)
	mustAdd(t, s, "Ana", "gosta de café sem açúcar", FlagOriginUser)
	if err := s.Supersede(ctx, old); err != nil {
		t.Fatal(err)
	}

	got, err := s.Search(ctx, "quando roda o backup?", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != backup {
		t.Fatalf("Search = %v, want only the active backup atom %d", ids(got), backup)
	}
}

func TestSearch_IgnoresDiacriticsAndCase(t *testing.T) {
	s := openTestStore(t)
	id := mustAdd(t, s, "Ana", "gosta de café sem açúcar", FlagOriginUser)
	got, err := s.Search(context.Background(), "CAFE", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("Search(CAFE) = %v, want [%d]", ids(got), id)
	}
}

// The query is the user's raw message: FTS5 syntax in it (quotes, -, :, *,
// NOT, OR, parentheses) must be treated as plain words, never as operators.
func TestSearch_RawMessageWithFTSSyntaxDoesNotError(t *testing.T) {
	s := openTestStore(t)
	mustAdd(t, s, "", "rode o comando de backup", FlagOriginUser)
	for _, msg := range []string{
		`rode o "comando" -x: NOT`,
		`backup* AND (disco OR`,
		`"`,
		`::: --- ***`,
		``,
	} {
		if _, err := s.Search(context.Background(), msg, 10); err != nil {
			t.Fatalf("Search(%q): %v", msg, err)
		}
	}
	got, _ := s.Search(context.Background(), `rode o "comando" -x: NOT`, 10)
	if len(got) != 1 {
		t.Fatalf("message with FTS syntax should still match by its words, got %v", ids(got))
	}
}

func TestSearch_RespectsLimit(t *testing.T) {
	s := openTestStore(t)
	for _, f := range []string{"backup um", "backup dois", "backup três"} {
		mustAdd(t, s, "", f, FlagOriginUser)
	}
	got, err := s.Search(context.Background(), "backup", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

// The FTS index is kept in sync by triggers, so atoms written by another
// connection (sleep subprocess, rule import) are searchable too.
func TestSearch_SeesAtomsWrittenByAnotherConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atoms.db")
	a, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	id := mustAdd(t, b, "VM", "o disco externo fica no rack", FlagOriginUser)
	got, err := a.Search(context.Background(), "rack", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("Search from the other connection = %v, want [%d]", ids(got), id)
	}
}

// A store created before the FTS index existed gets it built on open.
func TestOpenStore_BuildsIndexForExistingAtoms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atoms.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	id := mustAdd(t, s, "VM", "o rack fica na sala", FlagOriginUser)
	for _, stmt := range []string{
		`DROP TRIGGER atoms_fts_ai`, `DROP TRIGGER atoms_fts_ad`, `DROP TRIGGER atoms_fts_au`,
		`DROP TABLE atoms_fts`, `DELETE FROM meta WHERE key = 'fts_version'`,
	} {
		if _, err = s.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	s.Close()

	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Search(context.Background(), "rack", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("Search after reopen = %v, want [%d]", ids(got), id)
	}
}

func TestFloor_ReturnsPinnedRulesAndPreferencesOnly(t *testing.T) {
	s := openTestStore(t)
	rule := mustAdd(t, s, "", "nunca apague backups sem perguntar", FlagOriginUser)
	pref := mustAdd(t, s, "Ana", "prefere respostas curtas", FlagOriginUser)
	pinned := mustAdd(t, s, "VM", "a vm-ref tem 900M de MemoryMax", FlagOriginUser|FlagPinned)
	mustAdd(t, s, "VM", "o rack fica na sala", FlagOriginUser)

	got, err := s.Floor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]bool{rule: true, pref: true, pinned: true}
	if len(got) != len(want) {
		t.Fatalf("Floor = %v, want %v", ids(got), want)
	}
	for _, a := range got {
		if !want[a.ID] {
			t.Fatalf("Floor returned non-floor atom %d", a.ID)
		}
	}
}

func TestHasActive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if has, err := s.HasActive(ctx); err != nil || has {
		t.Fatalf("empty store: HasActive = %v, %v", has, err)
	}
	id := mustAdd(t, s, "", "um fato", FlagOriginUser)
	if has, _ := s.HasActive(ctx); !has {
		t.Fatal("HasActive = false with one active atom")
	}
	if err := s.Supersede(ctx, id); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasActive(ctx); has {
		t.Fatal("HasActive = true with only superseded atoms")
	}
}
