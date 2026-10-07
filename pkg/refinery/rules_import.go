package refinery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RulesImportResult reports one ImportRulesJSONL run.
type RulesImportResult struct {
	Imported int      // new pinned rule atoms
	Absorbed int      // rules already present (re-import)
	Rejected []string // "line N: reason" for lines that were skipped
}

// methodRule is the subset of a rules JSONL line (e.g. an interview
// method's rules) that becomes memory. Guidance, examples and limits are
// left to the conversation skill: the memory floor carries only the rule.
type methodRule struct {
	Tipo  string `json:"tipo"`
	ID    string `json:"id"`
	Regra string `json:"regra"`
}

// ImportRulesJSONL turns each {"tipo":"regra","id":...,"regra":...} line
// into a pinned rule atom keyed by its id (FR-039). Re-importing is
// idempotent: an identical rule is absorbed by the store's dedupe. Bad
// lines are reported, not fatal.
func ImportRulesJSONL(ctx context.Context, s *Store, r io.Reader) (RulesImportResult, error) {
	var res RulesImportResult
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rule methodRule
		if err := json.Unmarshal([]byte(line), &rule); err != nil {
			res.Rejected = append(res.Rejected, fmt.Sprintf("line %d: invalid JSON: %v", n, err))
			continue
		}
		if rule.Tipo != "regra" || strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Regra) == "" {
			res.Rejected = append(res.Rejected, fmt.Sprintf("line %d: not a rule with id and text", n))
			continue
		}
		_, absorbed, err := s.Add(ctx, rule.ID, rule.Regra, FlagPinned|FlagRule|FlagOriginImport)
		if err != nil {
			return res, fmt.Errorf("line %d: %w", n, err)
		}
		if absorbed {
			res.Absorbed++
		} else {
			res.Imported++
		}
	}
	return res, sc.Err()
}
