package refinery

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "atoms.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Atoms are cleaned and classified on the way in: rule/preference lines get
// their category bit, credentials the "sensitive" bit (kept, not removed).
func TestStore_AddClassifiesAndCleans(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	ruleID, _, err := s.Add(ctx, "Ana", "nunca envie mensagens\x1b[0m entre 23h e 7h", FlagOriginUser)
	if err != nil {
		t.Fatal(err)
	}
	keyID, _, err := s.Add(ctx, "VM", "a chave do provider é sk-proj-abcdefghijklmnop1234", FlagOriginUser)
	if err != nil {
		t.Fatal(err)
	}

	atoms, err := s.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]Atom{}
	for _, a := range atoms {
		byID[a.ID] = a
	}
	if r := byID[ruleID]; r.Flags&FlagRule == 0 || strings.Contains(r.Fact, "\x1b") {
		t.Fatalf("rule atom = %+v, want FlagRule and cleaned text", r)
	}
	if k := byID[keyID]; k.Flags&FlagSensitive == 0 || !strings.Contains(k.Fact, "sk-proj-abcdefghijklmnop1234") {
		t.Fatalf("credential atom = %+v, want FlagSensitive with the value kept", k)
	}
}

// AC-023-1 / AC-027-2: a repetition of the same fact (case, punctuation,
// spacing) is absorbed into the existing atom instead of stored twice.
func TestStore_RepeatedFactAbsorbed(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	first, absorbed, err := s.Add(ctx, "Ana", baseFact, FlagOriginUser)
	if err != nil || absorbed {
		t.Fatalf("first Add = (%d, %v, %v)", first, absorbed, err)
	}
	repeat := "o usuário prefere receber o relatório de carga às sete da manhã pelo telegram"
	again, absorbed, err := s.Add(ctx, "Ana", repeat, FlagOriginSleep)
	if err != nil {
		t.Fatal(err)
	}
	if !absorbed || again != first {
		t.Fatalf("repeated fact Add = (%d, absorbed=%v), want absorbed into %d", again, absorbed, first)
	}
	if atoms, _ := s.Active(ctx); len(atoms) != 1 {
		t.Fatalf("Active() has %d atoms, want 1", len(atoms))
	}
}

// A changed value ("sete" -> "oito") is a different fact, not a duplicate:
// merging it would silently drop the newer version. Lexical similarity is
// not logical equivalence (context-refinery ARCHITECTURE.md); resolving the
// conflict by timestamp belongs to consolidation (FR-023).
func TestStore_ChangedValueIsNotAbsorbed(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, _, _ = s.Add(ctx, "Ana", baseFact, FlagOriginUser)
	if _, absorbed, _ := s.Add(ctx, "Ana", nearFact, FlagOriginUser); absorbed {
		t.Fatal("a fact with a changed value was absorbed as a duplicate")
	}
	if atoms, _ := s.Active(ctx); len(atoms) != 2 {
		t.Fatalf("Active() has %d atoms, want 2", len(atoms))
	}
}

func TestStore_SupersededLeavesActiveSetAndDedupeIndex(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, _, _ := s.Add(ctx, "Ana", baseFact, FlagOriginUser)
	if err := s.Supersede(ctx, id); err != nil {
		t.Fatal(err)
	}
	if atoms, _ := s.Active(ctx); len(atoms) != 0 {
		t.Fatalf("superseded atom still active: %+v", atoms)
	}
	if _, absorbed, _ := s.Add(ctx, "Ana", baseFact, FlagOriginUser); absorbed {
		t.Fatal("a superseded atom still absorbed a new one")
	}
}

// P2: MEMORY.md is a view rendered from the atoms -- deterministic, rules
// and preferences first.
func TestRenderMarkdown_DeterministicRulesFirst(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	_, _, _ = s.Add(ctx, "VM", "a vm-ref tem 900M de MemoryMax", FlagOriginUser)
	_, _, _ = s.Add(ctx, "Ana", "prefere respostas curtas", FlagOriginUser)

	atoms, _ := s.Active(ctx)
	md := RenderMarkdown(atoms)
	if md != RenderMarkdown(atoms) {
		t.Fatal("RenderMarkdown is not deterministic")
	}
	pref := strings.Index(md, "- [Ana] prefere respostas curtas")
	fact := strings.Index(md, "- [VM] a vm-ref tem 900M de MemoryMax")
	if pref < 0 || fact < 0 || pref > fact {
		t.Fatalf("rendered MEMORY.md wrong or unordered:\n%s", md)
	}
}

// AC-022-7: edits made by hand (or by the agent's write_file) to the
// rendered MEMORY.md are imported before the next render -- an added line
// becomes a user-authored atom, a deleted line supersedes its atom.
func TestStore_SyncImportsManualEdits(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "MEMORY.md")

	_, _, _ = s.Add(ctx, "VM", "a vm-ref tem 900M de MemoryMax", FlagOriginUser)
	_, _, _ = s.Add(ctx, "Ana", "gosta de café sem açúcar", FlagOriginUser)
	if err := s.WriteMemoryFile(ctx, path); err != nil {
		t.Fatal(err)
	}

	edited := readFile(t, path)
	edited = strings.Replace(edited, "- [Ana] gosta de café sem açúcar\n", "", 1)
	edited += "- [Ana] mora em Recife\n"
	writeFile(t, path, edited)

	if err := s.SyncMemoryFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	atoms, _ := s.Active(ctx)
	var facts []string
	for _, a := range atoms {
		facts = append(facts, a.Fact)
		if a.Fact == "mora em Recife" && a.Flags&FlagUserAuthored == 0 {
			t.Fatalf("imported line not flagged user-authored: %+v", a)
		}
	}
	joined := strings.Join(facts, "|")
	if !strings.Contains(joined, "mora em Recife") || strings.Contains(joined, "café sem açúcar") {
		t.Fatalf("active facts after sync = %q", joined)
	}
}

func TestStore_SyncUnchangedFileIsNoop(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	_, _, _ = s.Add(ctx, "VM", "a vm-ref tem 900M de MemoryMax", FlagOriginUser)
	_ = s.WriteMemoryFile(ctx, path)

	if err := s.SyncMemoryFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	if atoms, _ := s.Active(ctx); len(atoms) != 1 {
		t.Fatalf("sync of an untouched file changed the atoms: %+v", atoms)
	}
}

// Today's MEMORY.md is free text written by the agent with write_file
// (paragraphs, not "- " items). Sync must import every content line, or
// the next render would overwrite the file without them -- data loss.
func TestStore_SyncImportsFreeTextLines(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	writeFile(t, path, "# Memória de longo prazo\n\nO usuário se chama Ana e mora em Recife.\n\n* Prefere respostas curtas.\n1. Backup roda às 3h.\n")

	if err := s.SyncMemoryFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteMemoryFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	rendered := readFile(t, path)
	for _, want := range []string{"O usuário se chama Ana e mora em Recife.", "Prefere respostas curtas.", "Backup roda às 3h."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("free-text line %q lost after sync+render:\n%s", want, rendered)
		}
	}
}

// Review finding: an atom is one line. A multi-line fact would be split
// apart by the next sync, so line breaks collapse on the way in.
func TestStore_MultiLineFactRoundTripsIntact(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	_, _, _ = s.Add(ctx, "VM", "o backup roda às 3h\ne vai para o disco externo", FlagOriginUser)
	_ = s.WriteMemoryFile(ctx, path)
	writeFile(t, path, readFile(t, path)+"\n") // force a sync pass

	if err := s.SyncMemoryFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	atoms, _ := s.Active(ctx)
	if len(atoms) != 1 || atoms[0].Fact != "o backup roda às 3h e vai para o disco externo" {
		t.Fatalf("multi-line fact after sync = %+v", atoms)
	}
}

// Review finding: a fact like "[Aviso] disco cheio" with no entity renders
// exactly like an entity line; normalizing on Add keeps render/parse
// idempotent instead of mutating the atom on the next sync.
func TestStore_BracketPrefixedFactIsStable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	id, _, _ := s.Add(ctx, "", "[Aviso] disco cheio", FlagOriginUser)
	_ = s.WriteMemoryFile(ctx, path)
	writeFile(t, path, readFile(t, path)+"\n")

	if err := s.SyncMemoryFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	atoms, _ := s.Active(ctx)
	if len(atoms) != 1 || atoms[0].ID != id {
		t.Fatalf("bracket-prefixed fact was replaced by sync: %+v", atoms)
	}
}

// Review finding: a second process (the FR-026 subprocess) writing the same
// database must not bypass dedupe through a stale in-memory index.
func TestStore_DedupeSeesOtherConnectionsWrites(t *testing.T) {
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
	ctx := context.Background()

	first, _, _ := a.Add(ctx, "Ana", baseFact, FlagOriginUser)
	again, absorbed, err := b.Add(ctx, "Ana", baseFact, FlagOriginSleep)
	if err != nil {
		t.Fatal(err)
	}
	if !absorbed || again != first {
		t.Fatalf("second connection Add = (%d, absorbed=%v), want absorbed into %d", again, absorbed, first)
	}
}
