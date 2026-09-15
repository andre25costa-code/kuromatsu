package agent

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// estimatePromptTokens mirrors tokenizer.EstimateMessageTokens' heuristic
// (chars*2/5) without importing pkg/tokenizer, which would pull
// pkg/providers into this already-heavy test package's import graph for no
// benefit — the two heuristics must stay numerically identical, not
// share an import.
func estimatePromptTokens(s string) int {
	return utf8.RuneCountInString(s) * 2 / 5
}

// TestGetIdentityCompact_UnderFrameworkOverheadBudget covers FR-015
// AC-015-1: the compact identity is ≤~150 tokens of framework overhead
// (budget relaxed to 180 here, matching the plan's own A9 calibration note
// — "150" in the ADR is an approximate target, not a hard contract).
func TestGetIdentityCompact_UnderFrameworkOverheadBudget(t *testing.T) {
	tmpDir := setupWorkspace(t, nil)
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	identity := cb.getIdentityCompact(true)
	if got := estimatePromptTokens(identity); got > 180 {
		t.Fatalf("getIdentityCompact() = %d tokens, want <=180\n%s", got, identity)
	}
	if strings.TrimSpace(identity) == "" {
		t.Fatal("getIdentityCompact() returned empty content")
	}
}

// TestGetIdentityCompact_SmallerThanFullIdentity ensures compact really is
// a reduction, not just a different rendering of the same content.
func TestGetIdentityCompact_SmallerThanFullIdentity(t *testing.T) {
	tmpDir := setupWorkspace(t, nil)
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	full := cb.getIdentity(true)
	compact := cb.getIdentityCompact(true)
	if len(compact) >= len(full) {
		t.Fatalf("compact identity (%d chars) not smaller than full identity (%d chars)", len(compact), len(full))
	}
}

// TestBuildMessagesFromPrompt_CompactSystemPromptUsesShortIdentity covers
// AC-015-1 end-to-end through BuildMessagesFromPrompt: CompactSystemPrompt
// swaps in the compact identity and drops the full one's tool-discovery
// verbosity.
func TestBuildMessagesFromPrompt_CompactSystemPromptUsesShortIdentity(t *testing.T) {
	tmpDir := setupWorkspace(t, nil)
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	messages := cb.BuildMessagesFromPrompt(PromptBuildRequest{
		CompactSystemPrompt: true,
		CurrentMessage:      "oi",
	})
	if len(messages) == 0 || messages[0].Role != "system" {
		t.Fatalf("messages = %#v, want a system message first", messages)
	}
	if strings.Contains(messages[0].Content, "Daily Notes") {
		t.Fatalf("compact system prompt still contains full-identity boilerplate:\n%s", messages[0].Content)
	}
	if got := estimatePromptTokens(messages[0].Content); got > 180 {
		t.Fatalf("compact system prompt (empty workspace) = %d tokens, want <=180\n%s", got, messages[0].Content)
	}
}

// TestBuildMessagesFromPrompt_DefaultRequestUnaffectedByFocusFields is the
// AC-014-9/AC-015-3 compatibility regression: a request that never sets
// any of the new fields must produce byte-identical output to before A6 —
// this is exactly the caching path (useDefaultCache) that must stay intact
// when focus.enabled=false.
func TestBuildMessagesFromPrompt_DefaultRequestUnaffectedByFocusFields(t *testing.T) {
	tmpDir := setupWorkspace(t, map[string]string{"AGENT.md": "# Agent\nHello."})
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	req := PromptBuildRequest{CurrentMessage: "oi"}
	first := cb.BuildMessagesFromPrompt(req)
	second := cb.BuildMessagesFromPrompt(req)
	if first[0].Content != second[0].Content {
		t.Fatalf("default request system prompt not stable across calls:\n1: %s\n2: %s", first[0].Content, second[0].Content)
	}
	if !strings.Contains(first[0].Content, "## Current Time") {
		t.Fatalf("default request lost the full dynamic context block:\n%s", first[0].Content)
	}
}

// TestBuildMessagesFromPrompt_CompactVariantIsStableAcrossCalls covers
// AC-015-3: the same non-default request shape (compact + a given
// MemoryMode) must produce the identical static prompt across calls on the
// same day, proving the variant cache (not just the default cache) is
// doing its job.
func TestBuildMessagesFromPrompt_CompactVariantIsStableAcrossCalls(t *testing.T) {
	tmpDir := setupWorkspace(t, map[string]string{"AGENT.md": "# Agent\nHello."})
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	req := PromptBuildRequest{CompactSystemPrompt: true, MemoryMode: config.FocusMemoryCore, CurrentMessage: "oi"}
	first := cb.BuildMessagesFromPrompt(req)
	second := cb.BuildMessagesFromPrompt(req)
	if first[0].Content != second[0].Content {
		t.Fatalf("compact variant not stable across calls:\n1: %s\n2: %s", first[0].Content, second[0].Content)
	}
}

// TestBuildMessagesFromPrompt_DynamicContextDateOnlyDropsMinuteTimestamp
// covers AC-015-2: DynamicContext="date" emits only "## Current Date", no
// Runtime/Session/Sender block and no per-minute timestamp.
func TestBuildMessagesFromPrompt_DynamicContextDateOnlyDropsMinuteTimestamp(t *testing.T) {
	tmpDir := setupWorkspace(t, nil)
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	messages := cb.BuildMessagesFromPrompt(PromptBuildRequest{
		DynamicContext: "date",
		Channel:        "telegram",
		ChatID:         "123",
		SenderID:       "u1",
		CurrentMessage: "oi",
	})
	system := messages[0].Content
	if !strings.Contains(system, "## Current Date") {
		t.Fatalf("date-only dynamic context missing Current Date block:\n%s", system)
	}
	if strings.Contains(system, "## Current Time") || strings.Contains(system, "## Runtime") ||
		strings.Contains(system, "## Current Session") || strings.Contains(system, "## Current Sender") {
		t.Fatalf("date-only dynamic context leaked full runtime/session/sender blocks:\n%s", system)
	}
}

// TestBuildMessagesFromPrompt_NeedsTimeStampsOnlyTheAssembledUserMessage
// covers ADR-014 point 3: the "[now: HH:MM]" stamp lands on the message
// object sent to the LLM, but the caller's own CurrentMessage string (what
// gets persisted to session history elsewhere) is never mutated.
func TestBuildMessagesFromPrompt_NeedsTimeStampsOnlyTheAssembledUserMessage(t *testing.T) {
	tmpDir := setupWorkspace(t, nil)
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	req := PromptBuildRequest{NeedsTime: true, CurrentMessage: "lembre de mim"}
	messages := cb.BuildMessagesFromPrompt(req)

	last := messages[len(messages)-1]
	if last.Role != "user" {
		t.Fatalf("last message role = %q, want user", last.Role)
	}
	if !strings.Contains(last.Content, "[now: ") {
		t.Fatalf("NeedsTime message missing [now: ...] stamp: %q", last.Content)
	}
	if !strings.HasPrefix(last.Content, "lembre de mim") {
		t.Fatalf("stamped message = %q, want original content preserved as a prefix", last.Content)
	}
	if req.CurrentMessage != "lembre de mim" {
		t.Fatalf("req.CurrentMessage mutated to %q, want unchanged (never persisted with the stamp)", req.CurrentMessage)
	}
}

// TestBuildMessagesFromPrompt_NeedsTimeFalseNeverStamps is the
// compatibility half: without NeedsTime, nothing changes.
func TestBuildMessagesFromPrompt_NeedsTimeFalseNeverStamps(t *testing.T) {
	tmpDir := setupWorkspace(t, nil)
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)

	messages := cb.BuildMessagesFromPrompt(PromptBuildRequest{CurrentMessage: "oi"})
	last := messages[len(messages)-1]
	if last.Content != "oi" {
		t.Fatalf("message = %q, want unchanged \"oi\"", last.Content)
	}
}

// TestBuildMessagesFromPrompt_MemoryModeCoreDropsDailyNotes covers ADR-014
// point 3's memory modes: "core" keeps MEMORY.md but drops recent daily
// notes; "off" drops memory context entirely; "" keeps today's full
// behavior.
func TestBuildMessagesFromPrompt_MemoryModeCoreDropsDailyNotes(t *testing.T) {
	tmpDir := setupWorkspace(t, map[string]string{
		"memory/MEMORY.md": "Long-term fact.",
	})
	defer os.RemoveAll(tmpDir)
	cb := NewContextBuilder(tmpDir)
	if err := cb.memory.AppendToday("Something that happened today."); err != nil {
		t.Fatalf("AppendToday() error = %v", err)
	}

	full := cb.BuildMessagesFromPrompt(PromptBuildRequest{CurrentMessage: "oi"})[0].Content
	if !strings.Contains(full, "Long-term fact.") || !strings.Contains(full, "Something that happened today.") {
		t.Fatalf("default memory mode missing long-term or daily content:\n%s", full)
	}

	core := cb.BuildMessagesFromPrompt(PromptBuildRequest{
		MemoryMode:     config.FocusMemoryCore,
		CurrentMessage: "oi",
	})[0].Content
	if !strings.Contains(core, "Long-term fact.") {
		t.Fatalf("core memory mode dropped MEMORY.md content:\n%s", core)
	}
	if strings.Contains(core, "Something that happened today.") {
		t.Fatalf("core memory mode leaked daily notes:\n%s", core)
	}

	off := cb.BuildMessagesFromPrompt(PromptBuildRequest{
		MemoryMode:     config.FocusMemoryOff,
		CurrentMessage: "oi",
	})[0].Content
	if strings.Contains(off, "Long-term fact.") || strings.Contains(off, "Something that happened today.") {
		t.Fatalf("off memory mode leaked memory content:\n%s", off)
	}
}
