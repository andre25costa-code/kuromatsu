package refinery

import (
	"context"
	"math"
	"math/bits"
	"sort"
	"strings"
)

// Retrieval defaults for FR-022 phase 2. Initial values, to be calibrated
// against the real corpus (ADR-020 P5): λ is the MMR redundancy weight on
// ĉos = cos(π·H/64) (the Python λ = 0.35 was tuned for Jaccard and does not
// carry over), DefaultSelectHamming is the k below which two atoms are never
// shown together, and DefaultMaxCandidates caps the FTS candidates per turn
// (AC-022-6).
const (
	DefaultMMRLambda     = 0.5
	DefaultSelectHamming = 3
	DefaultMaxCandidates = 50
)

// SelectOptions configures Select.
type SelectOptions struct {
	BudgetTokens   int
	Lambda         float64
	MinHamming     int
	EstimateTokens func(string) int
}

// Selection is the memory chosen for one prompt: the query-independent floor
// and the atoms retrieved for the current message.
type Selection struct {
	Floor          []Atom
	Retrieved      []Atom
	FloorTruncated bool // the floor alone exceeded the budget (AC-022-3)
	FloorTokens    int
}

// Select fills BudgetTokens with the floor first (pinned/rule/preference,
// newest kept when it does not fit, AC-022-3) and then with candidates by
// MMR: relevance from the candidates' order minus λ·max ĉos against what is
// already selected (AC-022-2). No two selected atoms are within MinHamming
// of each other. It works only on its inputs -- the floor and the capped
// candidates -- never on the whole store (AC-022-6), and calls no model.
func Select(floor, candidates []Atom, opt SelectOptions) Selection {
	var sel Selection
	budget := opt.BudgetTokens
	cost := func(a Atom) int { return opt.EstimateTokens(atomLine(a.Entity, a.Fact)) }
	selected := make([]uint64, 0, len(floor)+len(candidates))
	tooClose := func(fp uint64) bool {
		for _, s := range selected {
			if bits.OnesCount64(fp^s) <= opt.MinHamming {
				return true
			}
		}
		return false
	}

	// Floor, newest first, so a cut keeps the most recent rules.
	order := make([]int, len(floor))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return floor[order[i]].ID > floor[order[j]].ID })
	inFloor := make(map[int64]bool, len(floor))
	for _, i := range order {
		a := floor[i]
		inFloor[a.ID] = true
		if tooClose(a.SimHash) {
			continue
		}
		c := cost(a)
		if c > budget {
			sel.FloorTruncated = true
			continue
		}
		budget -= c
		sel.FloorTokens += c
		sel.Floor = append(sel.Floor, a)
		selected = append(selected, a.SimHash)
	}
	sort.Slice(sel.Floor, func(i, j int) bool { return sel.Floor[i].ID < sel.Floor[j].ID })

	// MMR over the candidates, best rank first.
	n := len(candidates)
	used := make([]bool, n)
	// Near-duplicate candidates are usually one fact with a changed value
	// ("às sete" -> "às oito"): only the newest may be shown, so a correction
	// is never hidden behind the stale version BM25 happened to rank higher.
	for i := range candidates {
		for j := range candidates {
			if candidates[j].ID > candidates[i].ID &&
				bits.OnesCount64(candidates[i].SimHash^candidates[j].SimHash) <= opt.MinHamming {
				used[i] = true
				break
			}
		}
	}
	for budget > 0 {
		best, bestScore := -1, math.Inf(-1)
		for i, a := range candidates {
			if used[i] {
				continue
			}
			if inFloor[a.ID] || tooClose(a.SimHash) {
				used[i] = true
				continue
			}
			relevance := 1 - float64(i)/float64(n)
			score := relevance - opt.Lambda*maxCos(a.SimHash, selected)
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		a := candidates[best]
		c := cost(a)
		if c > budget {
			continue
		}
		budget -= c
		sel.Retrieved = append(sel.Retrieved, a)
		selected = append(selected, a.SimHash)
	}
	return sel
}

// Retrieve is one turn's memory retrieval: the floor, up to maxCandidates
// FTS matches for message, and Select over both. It touches only those rows,
// so its cost does not grow with the size of the store (AC-022-6).
func Retrieve(ctx context.Context, s *Store, message string, maxCandidates int, opt SelectOptions) (Selection, error) {
	floor, err := s.Floor(ctx)
	if err != nil {
		return Selection{}, err
	}
	candidates, err := s.Search(ctx, message, maxCandidates)
	if err != nil {
		return Selection{}, err
	}
	return Select(floor, candidates, opt), nil
}

// maxCos is the highest ĉos = cos(π·H/64) between fp and the selected
// fingerprints, or 0 when nothing is selected yet.
func maxCos(fp uint64, selected []uint64) float64 {
	if len(selected) == 0 {
		return 0
	}
	best := -1.0
	for _, s := range selected {
		if c := math.Cos(math.Pi * float64(bits.OnesCount64(fp^s)) / 64); c > best {
			best = c
		}
	}
	return best
}

// RenderLines renders atoms as MEMORY.md lines ("- [Entity] fact").
func RenderLines(atoms []Atom) string {
	lines := make([]string, 0, len(atoms))
	for _, a := range atoms {
		lines = append(lines, atomLine(a.Entity, a.Fact))
	}
	return strings.Join(lines, "\n")
}
