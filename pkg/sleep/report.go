package sleep

import (
	"fmt"
	"strings"
	"time"
)

// Report is what one triage run produces. It is always written to
// <workspace>/state/sleep-report-<date>.md, whether or not DryRun applied
// the update, so a dry run's output is inspectable (FR-010/AC-010-2).
type Report struct {
	Date          time.Time
	WeeklyDeep    bool
	DryRun        bool
	SessionsSeen  int
	BatchesRun    int
	TokensUsed    int
	BudgetHit     bool
	MemoryUpdated bool
	Notes         []string
}

// Render formats the report as the Markdown file content.
func (r Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Relatório do modo dormir — %s\n\n", r.Date.Format("2006-01-02 15:04"))

	mode := "incremental"
	if r.WeeklyDeep {
		mode = "reorganização semanal completa"
	}
	fmt.Fprintf(&b, "- Modo: %s\n", mode)
	fmt.Fprintf(&b, "- Dry-run: %v\n", r.DryRun)
	fmt.Fprintf(&b, "- Sessões consideradas: %d\n", r.SessionsSeen)
	fmt.Fprintf(&b, "- Lotes processados: %d\n", r.BatchesRun)
	fmt.Fprintf(&b, "- Tokens usados: %d\n", r.TokensUsed)
	fmt.Fprintf(&b, "- Orçamento esgotado: %v\n", r.BudgetHit)
	fmt.Fprintf(&b, "- MEMORY.md atualizado: %v\n", r.MemoryUpdated)

	if len(r.Notes) > 0 {
		b.WriteString("\n## Notas\n\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	return b.String()
}

// ReportFileName returns the report's file name for a given date, e.g.
// "sleep-report-2026-09-10.md".
func ReportFileName(date time.Time) string {
	return fmt.Sprintf("sleep-report-%s.md", date.Format("2006-01-02"))
}
