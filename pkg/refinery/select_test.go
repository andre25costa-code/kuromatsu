package refinery

import (
	"context"
	"fmt"
	"math/bits"
	"strings"
	"testing"
)

// wordTokens is a deterministic stand-in for the agent's token estimator.
func wordTokens(s string) int { return len(strings.Fields(s)) }

func atom(id int64, fact string, flags uint32) Atom {
	return Atom{ID: id, Fact: fact, SimHash: SimHash64(fact), Flags: flags}
}

func selectOpts(budget int) SelectOptions {
	return SelectOptions{
		BudgetTokens:   budget,
		Lambda:         DefaultMMRLambda,
		MinHamming:     DefaultSelectHamming,
		EstimateTokens: wordTokens,
	}
}

func selectionTokens(sel Selection) int {
	total := 0
	for _, a := range append(append([]Atom(nil), sel.Floor...), sel.Retrieved...) {
		total += wordTokens(atomLine(a.Entity, a.Fact))
	}
	return total
}

// AC-022-3: floor atoms enter first and consume budget before retrieved ones.
func TestSelect_FloorFirstThenRetrieved(t *testing.T) {
	floor := []Atom{atom(1, "nunca apague backups sem perguntar", FlagRule)}
	cands := []Atom{
		atom(10, "o backup roda às três da manhã", 0),
		atom(11, "o rack fica na sala de servidores", 0),
	}
	sel := Select(floor, cands, selectOpts(100))
	if len(sel.Floor) != 1 || sel.Floor[0].ID != 1 {
		t.Fatalf("Floor = %v, want [1]", ids(sel.Floor))
	}
	if len(sel.Retrieved) != 2 {
		t.Fatalf("Retrieved = %v, want both candidates", ids(sel.Retrieved))
	}
	if sel.FloorTruncated {
		t.Fatal("FloorTruncated with plenty of budget")
	}
}

// AC-022-1 + AC-022-3: the floor alone over budget is cut by recency
// (newest kept), flagged, and nothing is retrieved on top of it.
func TestSelect_FloorOverBudgetKeepsNewest(t *testing.T) {
	floor := []Atom{
		atom(1, "regra antiga um dois três quatro cinco", FlagRule),
		atom(2, "regra do meio um dois três quatro", FlagRule),
		atom(3, "regra nova um dois três quatro", FlagRule),
	}
	cands := []Atom{atom(10, "qualquer fato recuperado", 0)}
	budget := wordTokens("- regra nova um dois três quatro") + wordTokens("- regra do meio um dois três quatro")
	sel := Select(floor, cands, selectOpts(budget))
	if !sel.FloorTruncated {
		t.Fatal("FloorTruncated = false although the floor exceeds the budget")
	}
	got := ids(sel.Floor)
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("Floor = %v, want the two newest [2 3] in id order", got)
	}
	if len(sel.Retrieved) != 0 {
		t.Fatalf("Retrieved = %v with no budget left", ids(sel.Retrieved))
	}
	if selectionTokens(sel) > budget {
		t.Fatalf("selection uses %d tokens, budget %d", selectionTokens(sel), budget)
	}
}

// AC-022-1: whatever the input size, the selection never exceeds the budget.
func TestSelect_NeverExceedsBudget(t *testing.T) {
	var cands []Atom
	for i := int64(0); i < 40; i++ {
		cands = append(
			cands,
			atom(100+i, "fato número "+strings.Repeat("x", int(i%7)+1)+" sobre o servidor e o backup", 0),
		)
	}
	for _, budget := range []int{0, 1, 7, 30, 75} {
		sel := Select(nil, cands, selectOpts(budget))
		if got := selectionTokens(sel); got > budget {
			t.Fatalf("budget %d: selection uses %d tokens", budget, got)
		}
	}
}

// AC-022-2: no two selected atoms within Hamming <= k, floor included.
func TestSelect_NeverTwoAtomsWithinMinHamming(t *testing.T) {
	base := "o backup roda às três da manhã no disco externo"
	floor := []Atom{atom(1, base, FlagPinned)}
	cands := []Atom{
		atom(10, base, 0),     // identical to the floor atom
		atom(11, base+".", 0), // same after normalization
		atom(12, "o rack fica na sala de servidores", 0),
	}
	sel := Select(floor, cands, selectOpts(200))
	all := append(append([]Atom(nil), sel.Floor...), sel.Retrieved...)
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if h := bits.OnesCount64(all[i].SimHash ^ all[j].SimHash); h <= DefaultSelectHamming {
				t.Fatalf("atoms %d and %d selected together at Hamming %d", all[i].ID, all[j].ID, h)
			}
		}
	}
	if len(sel.Retrieved) != 1 || sel.Retrieved[0].ID != 12 {
		t.Fatalf("Retrieved = %v, want only the distinct atom 12", ids(sel.Retrieved))
	}
}

// AC-022-2: MMR trades relevance for diversity. With room for two, a
// near-duplicate of the top hit loses to a less relevant but different atom.
func TestSelect_MMRPrefersDiverseOverNearDuplicate(t *testing.T) {
	top := atom(10, "o backup do servidor roda às três da manhã no disco externo", 0)
	near := atom(11, "o backup do servidor roda às quatro da manhã no disco externo", 0)
	other := atom(12, "a senha do roteador fica com a Ana", 0)
	h := bits.OnesCount64(top.SimHash ^ near.SimHash)
	if h <= DefaultSelectHamming {
		t.Skipf("fixture too close (Hamming %d): would be excluded by k, not by MMR", h)
	}
	budget := wordTokens(atomLine("", top.Fact)) + wordTokens(atomLine("", other.Fact))
	sel := Select(nil, []Atom{top, near, other}, selectOpts(budget))
	got := ids(sel.Retrieved)
	if len(got) != 2 || got[0] != 10 || got[1] != 12 {
		t.Fatalf("Retrieved = %v, want [10 12] (top hit, then the diverse atom)", got)
	}
}

func TestSelect_CandidateAlreadyInFloorIsNotRepeated(t *testing.T) {
	floor := []Atom{atom(1, "nunca apague backups sem perguntar", FlagRule)}
	cands := []Atom{floor[0], atom(10, "o rack fica na sala", 0)}
	sel := Select(floor, cands, selectOpts(100))
	for _, a := range sel.Retrieved {
		if a.ID == 1 {
			t.Fatal("floor atom repeated among retrieved")
		}
	}
}

func TestRenderLines(t *testing.T) {
	got := RenderLines([]Atom{{Entity: "Ana", Fact: "prefere respostas curtas"}, {Fact: "o rack fica na sala"}})
	want := "- [Ana] prefere respostas curtas\n- o rack fica na sala"
	if got != want {
		t.Fatalf("RenderLines = %q, want %q", got, want)
	}
}

// Select's work is bounded by its inputs (floor + capped candidates).
func TestSelect_AllocationsBoundedByCandidates(t *testing.T) {
	floor := []Atom{atom(1, "nunca apague backups sem perguntar", FlagRule)}
	var cands []Atom
	for i := int64(0); i < DefaultMaxCandidates; i++ {
		cands = append(cands, atom(100+i, "fato distinto número "+strings.Repeat("y", int(i)+1), 0))
	}
	opts := selectOpts(400)
	allocs := testing.AllocsPerRun(50, func() { Select(floor, cands, opts) })
	if allocs > float64(2*len(cands)) {
		t.Fatalf("Select allocates %.0f times for %d candidates", allocs, len(cands))
	}
}

// AC-022-6: a retrieval for one turn reads only the floor and the capped FTS
// candidates, so its allocations do not grow with the number of atoms stored.
func TestRetrieve_AllocationsDoNotGrowWithStoreSize(t *testing.T) {
	measure := func(n int) float64 {
		s := openTestStore(t)
		ctx := context.Background()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			fact := fmt.Sprintf("registro de manutenção número %d do equipamento %d", i, i%97)
			if _, err = tx.ExecContext(ctx,
				`INSERT INTO atoms (entity, fact, simhash, flags, ts) VALUES ('', ?, ?, 0, 0)`,
				fact, int64(SimHash64(fact))); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		mustAdd(t, s, "", "nunca desligue o servidor sem avisar", FlagOriginUser)
		opts := selectOpts(300)
		return testing.AllocsPerRun(20, func() {
			if _, err := Retrieve(ctx, s, "qual o backup do servidor hoje?", DefaultMaxCandidates, opts); err != nil {
				t.Fatal(err)
			}
		})
	}
	small, large := measure(200), measure(5000)
	if large > small*1.5+5 {
		t.Fatalf("allocations grew with the store: %.0f with 200 atoms, %.0f with 5000", small, large)
	}
}

func BenchmarkSelect50Candidates(b *testing.B) {
	floor := []Atom{atom(1, "nunca apague backups sem perguntar", FlagRule)}
	var cands []Atom
	for i := int64(0); i < 50; i++ {
		cands = append(cands, atom(100+i, "fato distinto número "+strings.Repeat("y", int(i)+1), 0))
	}
	opts := selectOpts(400)
	b.ReportAllocs()
	for b.Loop() {
		Select(floor, cands, opts)
	}
}

// Two candidates within Hamming k are usually the same fact with a changed
// value ("às sete" -> "às oito", Hamming 2): the newer one is the correction
// and must be the one shown, whatever order BM25 returned them in.
func TestSelect_NearDuplicatesKeepTheNewest(t *testing.T) {
	older := atom(10, baseFact, 0)
	newer := atom(11, nearFact, 0)
	if h := bits.OnesCount64(older.SimHash ^ newer.SimHash); h > DefaultSelectHamming {
		t.Fatalf("fixture not within k (Hamming %d > %d): adjust the facts", h, DefaultSelectHamming)
	}
	sel := Select(nil, []Atom{older, newer}, selectOpts(200)) // BM25 ranked the older first
	got := ids(sel.Retrieved)
	if len(got) != 1 || got[0] != 11 {
		t.Fatalf("Retrieved = %v, want only the newer fact [11]", got)
	}
}
