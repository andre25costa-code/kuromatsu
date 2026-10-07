package sleep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type acknowledgingSource struct {
	fakeSessionSource
	acked []SessionDigest
}

func (s *acknowledgingSource) Acknowledge(_ context.Context, _ string, digests []SessionDigest) error {
	s.acked = append(s.acked, digests...)
	return nil
}

func TestConsolidationFailureDoesNotAcknowledgePendingSessions(t *testing.T) {
	source := &acknowledgingSource{}
	for i := 0; i < 21; i++ {
		source.digests = append(source.digests, SessionDigest{SessionID: "session", Summary: "fact"})
	}
	calls := 0
	runtime := NewRuntime(Config{Enabled: true}, source, func(context.Context, string, string) (ChatResult, error) {
		calls++
		if calls == 2 {
			return ChatResult{}, errors.New("provider unavailable")
		}
		return ChatResult{Text: "saved first batch", TotalTokens: 100}, nil
	})
	workspace := t.TempDir()
	if runtime.RunColdPathOnce(context.Background(), workspace) == nil {
		t.Fatal("failure swallowed")
	}
	if len(source.acked) != 20 {
		t.Fatalf("acknowledged %d, want only successful batch", len(source.acked))
	}
	if !runtime.lastRun[workspace].IsZero() {
		t.Fatal("advanced full-run cursor on partial failure")
	}
	content, err := os.ReadFile(filepath.Join(workspace, memoryRelPath))
	if err != nil || string(content) != "saved first batch" {
		t.Fatalf("partial commit: %s %v", content, err)
	}
}

func TestCancelledConsolidationDoesNotPersistOrAcknowledge(t *testing.T) {
	source := &acknowledgingSource{
		fakeSessionSource: fakeSessionSource{digests: []SessionDigest{{SessionID: "s", Summary: "fact"}}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := NewRuntime(Config{Enabled: true}, source, func(context.Context, string, string) (ChatResult, error) {
		cancel()
		return ChatResult{Text: "must not persist", TotalTokens: 20}, nil
	})
	workspace := t.TempDir()
	if !errors.Is(runtime.RunColdPathOnce(ctx, workspace), context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if len(source.acked) != 0 {
		t.Fatal("canceled work acknowledged")
	}
	if _, err := os.Stat(filepath.Join(workspace, memoryRelPath)); !os.IsNotExist(err) {
		t.Fatal("canceled memory written")
	}
}

func TestBudgetReservesInputAndBoundsOutput(t *testing.T) {
	ctx := withTokenBudget(context.Background(), 1000)
	limit, err := OutputTokenLimit(ctx, "system", "user")
	if err != nil || limit != 478 {
		t.Fatalf("limit=%d err=%v", limit, err)
	}
	called := false
	_, report := runTriage(
		context.Background(),
		Config{MaxTokensBudget: 1000},
		false,
		"",
		[]SessionDigest{{Summary: strings.Repeat("x", 2000)}},
		func(context.Context, string, string) (ChatResult, error) {
			called = true
			return ChatResult{}, nil
		},
	)
	if called || !report.BudgetHit {
		t.Fatal("oversized input reached provider")
	}
}

func TestMissingUsageStillConsumesBudget(t *testing.T) {
	_, report := runTriage(
		context.Background(),
		Config{},
		false,
		"",
		[]SessionDigest{{Summary: "fact"}},
		fakeChat("memory", 0, nil),
	)
	if report.TokensUsed == 0 {
		t.Fatal("missing usage treated as free")
	}
}
