package refinery

import (
	"context"
	"strings"
	"testing"
)

const rulesFixture = `{"schema_versao":"1.0.0","tipo":"regra","id":"TM_CORE_01","titulo":"Investigar a vida da pessoa","regra":"Fale sobre a vida, o trabalho e os problemas reais da pessoa em vez de pedir uma avaliação da sua ideia.","limites":["O relato de um problema não demonstra, por si só, demanda pela sua solução."]}
{"schema_versao":"1.0.0","tipo":"regra","id":"TM_FUTURE_01","titulo":"Separar intenção de comportamento","regra":"Não use declarações hipotéticas sobre uso ou compra futura como validação suficiente de uma ideia ou solução."}

{"schema_versao":"1.0.0","tipo":"regra","id":"TM_NOPITCH_01","titulo":"Adiar a apresentação da solução","regra":"Durante a descoberta, evite apresentar ou defender sua solução antes de compreender o contexto, o problema e o comportamento da pessoa."}
`

// AC-039-1: each rule becomes a pinned rule atom keyed by its TM_* id.
func TestImportRulesJSONL_PinnedRuleAtoms(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	res, err := ImportRulesJSONL(ctx, s, strings.NewReader(rulesFixture))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 3 || res.Absorbed != 0 {
		t.Fatalf("ImportRulesJSONL() = %+v, want 3 imported", res)
	}

	atoms, _ := s.Active(ctx)
	byEntity := map[string]Atom{}
	for _, a := range atoms {
		byEntity[a.Entity] = a
	}
	core, ok := byEntity["TM_CORE_01"]
	if !ok || !strings.HasPrefix(core.Fact, "Fale sobre a vida") {
		t.Fatalf("TM_CORE_01 atom = %+v", core)
	}
	want := FlagPinned | FlagRule | FlagOriginImport
	if core.Flags&want != want {
		t.Fatalf("TM_CORE_01 flags = %b, want pinned|rule|import", core.Flags)
	}
}

// AC-039-1: importing again does not duplicate anything.
func TestImportRulesJSONL_Idempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	_, _ = ImportRulesJSONL(ctx, s, strings.NewReader(rulesFixture))

	res, err := ImportRulesJSONL(ctx, s, strings.NewReader(rulesFixture))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 || res.Absorbed != 3 {
		t.Fatalf("second import = %+v, want 0 imported / 3 absorbed", res)
	}
	if atoms, _ := s.Active(ctx); len(atoms) != 3 {
		t.Fatalf("Active() has %d atoms after re-import, want 3", len(atoms))
	}
}

// A malformed or incomplete line is rejected with its line number; the
// valid lines around it are still imported.
func TestImportRulesJSONL_RejectsBadLines(t *testing.T) {
	s := openTestStore(t)
	in := `{"tipo":"regra","id":"TM_OK_01","regra":"Pergunte sobre o último episódio concreto."}
{not json
{"tipo":"regra","id":"","regra":"sem id"}
{"tipo":"exemplo","id":"TM_X","regra":"outro tipo"}
`
	res, err := ImportRulesJSONL(context.Background(), s, strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || len(res.Rejected) != 3 {
		t.Fatalf("ImportRulesJSONL() = %+v, want 1 imported and 3 rejected", res)
	}
	if !strings.Contains(res.Rejected[0], "line 2") {
		t.Fatalf("rejection does not name the line: %q", res.Rejected[0])
	}
}
