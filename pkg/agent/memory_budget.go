package agent

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// memoryFloorPattern marks rule/preference lines that always survive the
// memory budget wherever they sit in the file (FR-022, ADR-020 §4b; the
// pt-BR terms come from context-refinery's exporters.py).
var memoryFloorPattern = regexp.MustCompile(`(?i)\b(prefere|preferência|gosta|odeia|evita|quer|deseja|deve|não deve|sempre|nunca|proibid[oa]|exigid[oa]|obrigatóri[oa]|regra|prefers?|likes?|hates?|avoids?|always|never|must|rule)\b`)

// memoryBudgetHeader opens a budgeted memory block. It is constant on
// purpose: anything that changes with the file (a token count, a date)
// would invalidate the cached floor that follows it on every append.
const memoryBudgetHeader = "_(Memória resumida automaticamente: o arquivo completo está em memory/MEMORY.md — " +
	"leia-o com read_file se precisar de algo que não aparece aqui.)_\n"

// estimateTextTokens is pkg/tokenizer's heuristic (2.5 chars/token) for a
// bare string.
func estimateTextTokens(s string) int {
	return utf8.RuneCountInString(s) * 2 / 5
}

// budgetMemoryContext caps the memory block once it grows past overTokens
// (FR-022 phase 1, AC-022-8/9). Priority: the file's first section
// (identity), then rule/preference lines newest first, then the most recent
// remaining lines -- all emitted in file order.
//
// The selection depends only on the content -- never on the turn's
// message -- so the block stays byte-identical across turns and keeps
// hitting the prefix cache: on the reference VM an uncached token costs ~1 s of
// prefill (S39 M9), so a per-message selection would add minutes per turn.
// A zero threshold or budget disables the cap.
func budgetMemoryContext(content string, overTokens, budgetTokens int) (string, bool) {
	if overTokens <= 0 || budgetTokens <= 0 || estimateTextTokens(content) <= overTokens {
		return content, false
	}

	lines := strings.Split(content, "\n")
	remaining := budgetTokens - lineTokens(memoryBudgetHeader)
	selected := map[int]bool{}
	take := func(i int) {
		if strings.TrimSpace(lines[i]) == "" || selected[i] {
			return
		}
		if cost := lineTokens(lines[i]); cost <= remaining {
			selected[i] = true
			remaining -= cost
		}
	}

	sectionEnd := firstSectionEnd(lines)
	for i := 0; i < sectionEnd; i++ {
		take(i)
	}
	for i := len(lines) - 1; i >= sectionEnd; i-- {
		if memoryFloorPattern.MatchString(lines[i]) {
			take(i)
		}
	}
	for i := len(lines) - 1; i >= 0 && remaining > 0; i-- {
		take(i)
	}

	if len(selected) == 0 {
		// Nothing fits whole (e.g. one huge line): keep the most recent
		// part of the last non-blank line rather than an empty block.
		return memoryBudgetHeader + lastLineTail(lines, remaining-1) + "\n", true
	}

	kept := make([]int, 0, len(selected))
	for i := range selected {
		kept = append(kept, i)
	}
	sort.Ints(kept)

	withBreaks := renderKeptLines(lines, kept, true)
	if estimateTextTokens(withBreaks) <= budgetTokens {
		return withBreaks, true
	}
	return renderKeptLines(lines, kept, false), true
}

// renderKeptLines writes the header and the kept lines in file order. With
// breaks, two kept lines that had a blank line between them in the file
// (with or without omitted lines) are separated by one, so paragraphs and
// gaps stay visible.
func renderKeptLines(lines []string, kept []int, breaks bool) string {
	var b strings.Builder
	b.WriteString(memoryBudgetHeader)
	for k, i := range kept {
		if breaks && k > 0 && blankBetween(lines, kept[k-1], i) {
			b.WriteByte('\n')
		}
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	return b.String()
}

func blankBetween(lines []string, from, to int) bool {
	for j := from + 1; j < to; j++ {
		if strings.TrimSpace(lines[j]) == "" {
			return true
		}
	}
	return false
}

// lineTokens is estimateTextTokens rounded up, plus the newline, so the
// per-line costs never sum below the estimate of the rendered block.
func lineTokens(line string) int {
	return (utf8.RuneCountInString(line)*2+4)/5 + 1
}

// lastLineTail returns the end of the last non-blank line, cut to about
// budgetTokens.
func lastLineTail(lines []string, budgetTokens int) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			runes := []rune(line)
			if keep := budgetTokens * 5 / 2; keep < len(runes) {
				runes = runes[len(runes)-max(keep, 0):]
			}
			return string(runes)
		}
	}
	return ""
}

// firstSectionEnd returns the index of the second markdown heading -- the
// end of the file's opening section (identity/summary). With fewer than two
// headings there is no separate opening section: only the first line counts.
func firstSectionEnd(lines []string) int {
	seenHeading := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			if seenHeading {
				return i
			}
			seenHeading = true
		}
	}
	return min(len(lines), 1)
}
