package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// bigMemory builds a MEMORY.md-shaped document: an identity section, a
// rule buried in the middle, a lot of knowledge lines, and recent notes
// at the end.
func bigMemory() string {
	var b strings.Builder
	b.WriteString("# Memória\n\nO usuário se chama Ana e mora em Recife.\n\n## Notas\n\n")
	for i := 0; i < 200; i++ {
		if i == 100 {
			b.WriteString("- Regra: nunca envie mensagens entre 23h e 7h.\n")
			continue
		}
		fmt.Fprintf(&b, "- Fato antigo número %d sobre um assunto qualquer do passado.\n", i)
	}
	b.WriteString("- Nota mais recente: comprar flores no aniversário.\n")
	return b.String()
}

func TestBudgetMemoryContext_UnderThresholdUnchanged(t *testing.T) {
	small := "# Memória\n\n- O usuário prefere respostas curtas.\n"
	got, trimmed := budgetMemoryContext(small, 1200, 600)
	if trimmed || got != small {
		t.Fatalf("budgetMemoryContext() = (%q, %v), want the input unchanged", got, trimmed)
	}
}

func TestBudgetMemoryContext_DisabledWithZeroThreshold(t *testing.T) {
	big := bigMemory()
	if got, trimmed := budgetMemoryContext(big, 0, 600); trimmed || got != big {
		t.Fatal("threshold 0 must disable the budget")
	}
}

// AC-022-1/8/9: over the threshold the block fits the budget, keeps the
// floor (first section + rule/preference lines, wherever they are) and the
// most recent lines, and says where the full file is.
func TestBudgetMemoryContext_OverThresholdKeepsFloorAndRecent(t *testing.T) {
	got, trimmed := budgetMemoryContext(bigMemory(), 1200, 600)
	if !trimmed {
		t.Fatal("trimmed = false for a memory well over the threshold")
	}
	if tokens := estimateTextTokens(got); tokens > 600 {
		t.Fatalf("budgeted memory = %d tokens, want <= 600", tokens)
	}
	for _, want := range []string{
		"O usuário se chama Ana",               // first section
		"nunca envie mensagens entre 23h e 7h", // rule from the middle
		"Nota mais recente: comprar flores",    // most recent line
		"memory/MEMORY.md",                     // pointer to the full file
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("budgeted memory missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Fato antigo número 5 ") {
		t.Fatal("budgeted memory kept old knowledge lines from the middle")
	}
}

// The selection must not depend on anything but the file content, so the
// block stays byte-identical across turns and keeps hitting the prefix
// cache (S39 M9: uncached prefill runs at ~1 tok/s on the reference VM).
func TestBudgetMemoryContext_IsDeterministic(t *testing.T) {
	a, _ := budgetMemoryContext(bigMemory(), 1200, 600)
	b, _ := budgetMemoryContext(bigMemory(), 1200, 600)
	if a != b {
		t.Fatal("budgetMemoryContext() is not deterministic")
	}
}

func TestBudgetMemoryContext_FloorOverBudgetKeepsMostRecentFloor(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Memória\n\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "- Regra %d: sempre confirme antes de agir no item %d.\n", i, i)
	}
	got, _ := budgetMemoryContext(b.String(), 1200, 400)
	if tokens := estimateTextTokens(got); tokens > 400 {
		t.Fatalf("budgeted memory = %d tokens, want <= 400", tokens)
	}
	if !strings.Contains(got, "Regra 299:") || strings.Contains(got, "Regra 0:") {
		t.Fatalf("floor over budget must keep the most recent floor lines:\n%s", got)
	}
}

// Wiring: the system prompt's memory part goes through the budget.
func TestContextBuilder_MemoryBudgetAppliesToSystemPrompt(t *testing.T) {
	workspace := t.TempDir()
	cb := NewContextBuilder(workspace).WithMemoryBudget(1200, 600)
	if err := cb.memory.WriteLongTerm(bigMemory()); err != nil {
		t.Fatal(err)
	}

	prompt := cb.BuildSystemPrompt()
	if !strings.Contains(prompt, "Memória resumida automaticamente") ||
		!strings.Contains(prompt, "nunca envie mensagens entre 23h e 7h") {
		t.Fatal("system prompt memory was not budgeted")
	}
	if strings.Contains(prompt, "Fato antigo número 5 ") {
		t.Fatal("system prompt still carries the full MEMORY.md")
	}
}

func TestDefaultConfig_MemoryBudgetDefaults(t *testing.T) {
	d := config.DefaultConfig().Agents.Defaults
	if d.MemoryBudgetOverTokens != 1200 || d.MemoryBudgetTokens != 600 {
		t.Fatalf("memory budget defaults = (%d, %d), want (1200, 600)", d.MemoryBudgetOverTokens, d.MemoryBudgetTokens)
	}
}

// Review finding: the header must not change when the file grows, or every
// append invalidates the cached floor that follows it.
func TestBudgetMemoryContext_HeaderStableAcrossAppends(t *testing.T) {
	a, _ := budgetMemoryContext(bigMemory(), 1200, 600)
	b, _ := budgetMemoryContext(bigMemory()+"- Mais uma nota.\n", 1200, 600)
	headerA, headerB := a[:strings.Index(a, "\n")], b[:strings.Index(b, "\n")]
	if headerA != headerB {
		t.Fatalf("header changed on append:\n%q\n%q", headerA, headerB)
	}
}

// Review finding: with a tight budget the identity section must win over
// rule lines from the end of the file.
func TestBudgetMemoryContext_IdentityKeptBeforeLaterRules(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Memória\n\nO usuário se chama Ana, mora em Recife, trabalha com infraestrutura e estuda agentes de IA e LLMs.\n\n## Regras\n\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "- Regra %d: sempre confirme antes de agir.\n", i)
	}
	got, _ := budgetMemoryContext(b.String(), 1200, 150)
	if !strings.Contains(got, "O usuário se chama Ana") {
		t.Fatalf("identity section dropped under a tight budget:\n%s", got)
	}
}

// Review finding: one huge line larger than the budget must not leave the
// block empty -- its most recent part is kept.
func TestBudgetMemoryContext_SingleHugeLineKeepsItsTail(t *testing.T) {
	huge := strings.Repeat("fato ", 3000) + "FIM"
	got, trimmed := budgetMemoryContext(huge, 1200, 600)
	if !trimmed || !strings.Contains(got, "FIM") || estimateTextTokens(got) > 600 {
		t.Fatalf("huge single line: trimmed=%v tokens=%d hasTail=%v", trimmed, estimateTextTokens(got), strings.Contains(got, "FIM"))
	}
}

// Review finding: blank lines between kept lines are preserved so the
// markdown keeps its paragraphs.
func TestBudgetMemoryContext_KeepsBlankLinesBetweenKeptLines(t *testing.T) {
	got, _ := budgetMemoryContext(bigMemory(), 1200, 600)
	if !strings.Contains(got, "O usuário se chama Ana e mora em Recife.\n\n") {
		t.Fatalf("paragraph break after the identity line was lost:\n%s", got)
	}
}
