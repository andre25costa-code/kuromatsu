package sleep

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SessionDigest is one already-condensed conversation the triage pipeline
// considers for consolidation. Producing digests (e.g. from JSONL session
// files or seahorse) is the caller's responsibility, keeping this package
// decoupled from pkg/memory/pkg/seahorse (mirrors the localllm/provider.go
// pattern: this package defines the seam, the wiring lives elsewhere).
type SessionDigest struct {
	SessionID string
	Summary   string
	Revision  string // Opaque snapshot acknowledged only after persistence.
}

// SessionSource supplies session digests for a workspace.
type SessionSource interface {
	// RecentDigests returns digests touched at or after since. If
	// weeklyDeep is true, since is the zero time (consider all history).
	RecentDigests(ctx context.Context, workspace string, since time.Time, weeklyDeep bool) ([]SessionDigest, error)
}

// ChatResult is one LLM call's outcome.
type ChatResult struct {
	Text        string
	TotalTokens int
}

// ChatFunc issues one LLM call to the explicitly configured external model.
// Implementations must honor OutputTokenLimit from the call context.
type ChatFunc func(ctx context.Context, systemPrompt, userPrompt string) (ChatResult, error)

// digestsPerCall caps how many session digests go into a single
// consolidation call, bounding both the prompt size and how many calls a
// weekly_deep run needs against MaxTokensBudget (BR-006).
const digestsPerCall = 20

const consolidationSystemPrompt = `Você consolida a memória de longo prazo de um agente pessoal.
Vai receber o conteúdo atual de MEMORY.md e resumos de sessões recentes.
Devolva o conteúdo ATUALIZADO e COMPLETO de MEMORY.md: preserve fatos ainda
relevantes, remova o que ficou obsoleto ou duplicado, e incorpore fatos
novos e estáveis vistos nas sessões (reforço). Não invente informação que
não esteja no material recebido. Responda apenas com o novo conteúdo do
arquivo, sem comentários adicionais.`

// runTriage executes one collect -> consolidate pass over already-fetched
// digests and the current MEMORY.md content, returning the (possibly
// unchanged) updated content plus a Report. It never touches the
// filesystem; callers decide whether/where to persist the result
// (Runtime.RunColdPathOnce does, respecting cfg.DryRun).
func runTriage(
	ctx context.Context,
	cfg Config,
	weeklyDeep bool,
	currentMemory string,
	digests []SessionDigest,
	chat ChatFunc,
) (updatedMemory string, report Report) {
	report = Report{Date: time.Now(), WeeklyDeep: weeklyDeep, DryRun: cfg.DryRun, SessionsSeen: len(digests)}
	updatedMemory = currentMemory

	if len(digests) == 0 {
		report.Notes = append(report.Notes, "nenhuma sessão nova desde a última rodada; nada a consolidar")
		return updatedMemory, report
	}

	budget := cfg.WithDefaults().MaxTokensBudget

	for start := 0; start < len(digests); {
		if err := ctx.Err(); err != nil {
			report.Err = err
			break
		}
		if report.TokensUsed >= budget {
			report.BudgetHit = true
			report.Notes = append(report.Notes, fmt.Sprintf(
				"orcamento de %d tokens esgotado apos %d lote(s); %d sessao(oes) nao processada(s)",
				budget, report.BatchesRun, len(digests)-start))
			break
		}

		end := min(start+digestsPerCall, len(digests))
		prompt := renderConsolidationPrompt(updatedMemory, digests[start:end])
		callCtx := withTokenBudget(ctx, budget-report.TokensUsed)
		// Reduce the batch to fit the available input reservation; never
		// discard a digest merely because its neighbors made a large batch.
		for end > start+1 {
			if _, err := OutputTokenLimit(callCtx, consolidationSystemPrompt, prompt); err == nil {
				break
			}
			end--
			prompt = renderConsolidationPrompt(updatedMemory, digests[start:end])
		}
		if _, err := OutputTokenLimit(callCtx, consolidationSystemPrompt, prompt); err != nil {
			report.BudgetHit = true
			report.Err = err
			break
		}
		result, err := chat(callCtx, consolidationSystemPrompt, prompt)
		if err != nil {
			report.Err = err
			report.Notes = append(
				report.Notes,
				fmt.Sprintf("lote %d: chamada ao modelo falhou: %v", report.BatchesRun+1, err),
			)
			break
		}
		report.BatchesRun++
		if result.TotalTokens <= 0 {
			result.TotalTokens = len(consolidationSystemPrompt) + len(prompt) + len(result.Text) + 512
		}
		report.TokensUsed += result.TotalTokens
		if text := strings.TrimSpace(result.Text); text != "" {
			updatedMemory = text
			report.ProcessedDigests = append(report.ProcessedDigests, digests[start:end]...)
		} else {
			report.Err = fmt.Errorf("sleep model returned empty memory")
			break
		}
		start = end
	}

	report.MemoryUpdated = updatedMemory != currentMemory
	return updatedMemory, report
}

func renderConsolidationPrompt(currentMemory string, digests []SessionDigest) string {
	var b strings.Builder
	b.WriteString("## MEMORY.md atual\n\n")
	if currentMemory == "" {
		b.WriteString("(vazio)\n")
	} else {
		b.WriteString(currentMemory)
		b.WriteString("\n")
	}
	b.WriteString("\n## Sessões recentes\n\n")
	for _, d := range digests {
		fmt.Fprintf(&b, "### Sessão %s\n\n%s\n\n", d.SessionID, d.Summary)
	}
	return b.String()
}
