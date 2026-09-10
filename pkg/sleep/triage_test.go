package sleep

import (
	"context"
	"fmt"
	"testing"
)

func fakeChat(text string, tokens int, err error) ChatFunc {
	return func(context.Context, string, string) (ChatResult, error) {
		return ChatResult{Text: text, TotalTokens: tokens}, err
	}
}

func TestRunTriage_NoDigests_NoChange(t *testing.T) {
	updated, report := runTriage(context.Background(), Config{}, false, "memória antiga", nil, fakeChat("nunca chamado", 0, nil))
	if updated != "memória antiga" {
		t.Fatalf("updated = %q, want unchanged", updated)
	}
	if report.MemoryUpdated {
		t.Fatal("MemoryUpdated should be false with no digests")
	}
	if report.BatchesRun != 0 {
		t.Fatalf("BatchesRun = %d, want 0", report.BatchesRun)
	}
}

func TestRunTriage_SingleBatch_UpdatesMemory(t *testing.T) {
	digests := []SessionDigest{{SessionID: "s1", Summary: "usuário pediu lembrete de estudo às 8h"}}
	chat := fakeChat("# Memória\n\n- Lembrete de estudo às 8h", 120, nil)

	updated, report := runTriage(context.Background(), Config{}, false, "", digests, chat)

	if updated != "# Memória\n\n- Lembrete de estudo às 8h" {
		t.Fatalf("updated = %q", updated)
	}
	if !report.MemoryUpdated {
		t.Fatal("expected MemoryUpdated = true")
	}
	if report.BatchesRun != 1 || report.TokensUsed != 120 {
		t.Fatalf("BatchesRun=%d TokensUsed=%d", report.BatchesRun, report.TokensUsed)
	}
	if report.BudgetHit {
		t.Fatal("BudgetHit should be false")
	}
}

func TestRunTriage_EmptyReply_KeepsPreviousMemory(t *testing.T) {
	digests := []SessionDigest{{SessionID: "s1", Summary: "nada relevante"}}
	chat := fakeChat("   ", 10, nil) // blank reply after trimming

	updated, report := runTriage(context.Background(), Config{}, false, "memória original", digests, chat)

	if updated != "memória original" {
		t.Fatalf("updated = %q, want unchanged on a blank reply", updated)
	}
	if report.MemoryUpdated {
		t.Fatal("MemoryUpdated should be false when the reply is blank")
	}
}

func TestRunTriage_MultipleBatches_UnderBudget(t *testing.T) {
	digests := make([]SessionDigest, digestsPerCall*2+1) // forces 3 batches
	for i := range digests {
		digests[i] = SessionDigest{SessionID: fmt.Sprintf("s%d", i), Summary: "resumo"}
	}
	calls := 0
	chat := ChatFunc(func(context.Context, string, string) (ChatResult, error) {
		calls++
		return ChatResult{Text: fmt.Sprintf("memória v%d", calls), TotalTokens: 100}, nil
	})

	updated, report := runTriage(context.Background(), Config{MaxTokensBudget: 10000}, false, "", digests, chat)

	if calls != 3 {
		t.Fatalf("chat called %d times, want 3", calls)
	}
	if report.BatchesRun != 3 {
		t.Fatalf("BatchesRun = %d, want 3", report.BatchesRun)
	}
	if updated != "memória v3" {
		t.Fatalf("updated = %q, want the last batch's reply", updated)
	}
	if report.BudgetHit {
		t.Fatal("BudgetHit should be false when comfortably under budget")
	}
}

// BR-006 / AC-010-3: the budget is a hard stop mid-run, with a partial
// report noting how much was left unprocessed.
func TestRunTriage_StopsWhenBudgetExhausted(t *testing.T) {
	digests := make([]SessionDigest, digestsPerCall*3) // would need 3 batches
	for i := range digests {
		digests[i] = SessionDigest{SessionID: fmt.Sprintf("s%d", i), Summary: "resumo"}
	}
	calls := 0
	chat := ChatFunc(func(context.Context, string, string) (ChatResult, error) {
		calls++
		return ChatResult{Text: fmt.Sprintf("memória v%d", calls), TotalTokens: 6000}, nil
	})

	_, report := runTriage(context.Background(), Config{MaxTokensBudget: 10000}, false, "", digests, chat)

	if calls != 2 {
		t.Fatalf("chat called %d times, want exactly 2 (budget exhausted after the 2nd)", calls)
	}
	if !report.BudgetHit {
		t.Fatal("expected BudgetHit = true")
	}
	if len(report.Notes) == 0 {
		t.Fatal("expected a note explaining the partial run")
	}
}

func TestRunTriage_ChatError_StopsAndNotes(t *testing.T) {
	digests := []SessionDigest{{SessionID: "s1", Summary: "x"}}
	chat := fakeChat("", 0, fmt.Errorf("provider unavailable"))

	updated, report := runTriage(context.Background(), Config{}, false, "memória", digests, chat)

	if updated != "memória" {
		t.Fatalf("updated = %q, want unchanged after an error", updated)
	}
	if report.MemoryUpdated {
		t.Fatal("MemoryUpdated should be false after an error")
	}
	if len(report.Notes) == 0 {
		t.Fatal("expected a note describing the failure")
	}
}

func TestRunTriage_WeeklyDeepFlagReflectedInReport(t *testing.T) {
	digests := []SessionDigest{{SessionID: "s1", Summary: "x"}}
	_, report := runTriage(context.Background(), Config{}, true, "", digests, fakeChat("y", 1, nil))
	if !report.WeeklyDeep {
		t.Fatal("expected report.WeeklyDeep = true")
	}
}
