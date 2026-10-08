package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/refinery"
)

const retrievedMemoryMD = "# Memória\n\n" +
	"- nunca apague backups sem perguntar\n" +
	"- [Ana] prefere respostas curtas\n" +
	"- [VM] o rack fica na sala de servidores\n" +
	"- [VM] o backup roda às três da manhã no disco externo\n"

func retrievedRequest(msg string) PromptBuildRequest {
	return PromptBuildRequest{
		CompactSystemPrompt: true,
		MemoryMode:          config.FocusMemoryRetrieved,
		CurrentMessage:      msg,
	}
}

func newRetrievedBuilder(t *testing.T, memory string) (*ContextBuilder, string) {
	t.Helper()
	files := map[string]string{"AGENT.md": "# Agent\nTest."}
	if memory != "" {
		files["memory/MEMORY.md"] = memory
	}
	ws := setupWorkspace(t, files)
	t.Cleanup(func() { os.RemoveAll(ws) })
	cb := NewContextBuilder(ws).WithMemoryBudget(1200, 600)
	t.Cleanup(func() { _ = cb.Close() })
	return cb, ws
}

// AC-022-3 + AC-022-10: the query-independent floor (rules, preferences,
// pinned) is part of the cached system prompt; facts are not.
func TestRetrievedMemory_FloorInSystemPrompt(t *testing.T) {
	cb, _ := newRetrievedBuilder(t, retrievedMemoryMD)
	prompt, _ := cb.buildSystemPromptForRequest(retrievedRequest("oi"))
	if !strings.Contains(prompt, "nunca apague backups sem perguntar") ||
		!strings.Contains(prompt, "prefere respostas curtas") {
		t.Fatalf("floor atoms missing from the system prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "o rack fica na sala") {
		t.Fatalf("non-floor fact leaked into the static system prompt:\n%s", prompt)
	}
}

// AC-022-10: atoms retrieved for the message go after every stable part, in
// the user message of this call only, and never into what gets persisted.
func TestRetrievedMemory_PerTurnBlockInUserMessageOnly(t *testing.T) {
	cb, _ := newRetrievedBuilder(t, retrievedMemoryMD)
	req := retrievedRequest("onde fica o rack?")
	msgs := cb.BuildMessagesFromPrompt(req)
	last := msgs[len(msgs)-1]
	if last.Role != "user" {
		t.Fatalf("last message role = %q", last.Role)
	}
	if !strings.Contains(last.Content, "o rack fica na sala de servidores") {
		t.Fatalf("retrieved atom missing from the user message:\n%s", last.Content)
	}
	if !strings.HasSuffix(last.Content, "onde fica o rack?") {
		t.Fatalf("the user's own text must come last and unchanged:\n%s", last.Content)
	}
	if req.CurrentMessage != "onde fica o rack?" {
		t.Fatalf("request mutated: CurrentMessage = %q", req.CurrentMessage)
	}
	if strings.Contains(msgs[0].Content, "o rack fica na sala") {
		t.Fatal("retrieved atom also placed in the system message")
	}
}

// AC-022-1: floor + retrieved stay within memory_budget_tokens.
func TestRetrievedMemory_RespectsBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Memória\n\n")
	for i := 0; i < 200; i++ {
		b.WriteString("- [VM] registro de manutenção do rack número ")
		b.WriteString(strings.Repeat("x", i%13+1))
		b.WriteString("\n")
	}
	ws := setupWorkspace(t, map[string]string{"memory/MEMORY.md": b.String()})
	defer os.RemoveAll(ws)
	const budget = 80
	cb := NewContextBuilder(ws).WithMemoryBudget(1200, budget)
	defer cb.Close()

	msgs := cb.BuildMessagesFromPrompt(retrievedRequest("manutenção do rack"))
	content := msgs[len(msgs)-1].Content
	block := strings.TrimSuffix(content, "\n\nmanutenção do rack")
	if block == content {
		t.Fatalf("user text not at the end of the message:\n%s", content)
	}
	if got := estimateTextTokens(block); got > budget {
		t.Fatalf("retrieved memory block (header included) ~%d tokens, budget %d", got, budget)
	}
	if !strings.Contains(block, "registro de manutenção do rack") {
		t.Fatalf("nothing retrieved:\n%s", block)
	}
}

// Cache: a new rule written straight to the atom store (sleep, rule import)
// must appear in the next prompt, although no workspace file changed.
func TestRetrievedMemory_NewFloorAtomInvalidatesCachedPrompt(t *testing.T) {
	cb, ws := newRetrievedBuilder(t, retrievedMemoryMD)
	first, _ := cb.buildSystemPromptForRequest(retrievedRequest("oi"))

	other, err := refinery.OpenStore(filepath.Join(ws, "memory", "atoms.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := other.Add(
		context.Background(),
		"",
		"nunca reinicie o servidor em horário comercial",
		refinery.FlagOriginImport,
	); err != nil {
		t.Fatal(err)
	}
	other.Close()

	second, _ := cb.buildSystemPromptForRequest(retrievedRequest("oi"))
	if second == first || !strings.Contains(second, "nunca reinicie o servidor") {
		t.Fatalf("cached prompt kept the old floor:\n%s", second)
	}
}

// AC-022-7: a manual edit to MEMORY.md becomes an atom before the next
// retrieved turn, and the file itself is never rewritten by a turn.
func TestRetrievedMemory_ManualEditIsImportedAndFileUntouched(t *testing.T) {
	cb, ws := newRetrievedBuilder(t, retrievedMemoryMD)
	cb.BuildMessagesFromPrompt(retrievedRequest("oi"))

	path := filepath.Join(ws, "memory", "MEMORY.md")
	edited := retrievedMemoryMD + "- [Impressora] o toner fica no armário azul\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := cb.BuildMessagesFromPrompt(retrievedRequest("onde fica o toner?"))
	if !strings.Contains(msgs[len(msgs)-1].Content, "o toner fica no armário azul") {
		t.Fatalf("manual edit not retrieved:\n%s", msgs[len(msgs)-1].Content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != edited {
		t.Fatal("a turn rewrote MEMORY.md")
	}
}

// AC-022-5: with no atoms (and nothing to import) retrieved falls back to
// core: same static prompt, untouched user message.
func TestRetrievedMemory_EmptyStoreFallsBackToCore(t *testing.T) {
	cb, _ := newRetrievedBuilder(t, "")
	retrieved, _ := cb.buildSystemPromptForRequest(retrievedRequest("oi"))
	coreReq := retrievedRequest("oi")
	coreReq.MemoryMode = config.FocusMemoryCore
	core, _ := cb.buildSystemPromptForRequest(coreReq)
	if retrieved != core {
		t.Fatalf(
			"empty store: retrieved prompt differs from core\n--- retrieved ---\n%s\n--- core ---\n%s",
			retrieved,
			core,
		)
	}
	msgs := cb.BuildMessagesFromPrompt(retrievedRequest("oi"))
	if msgs[len(msgs)-1].Content != "oi" {
		t.Fatalf("empty store: user message altered: %q", msgs[len(msgs)-1].Content)
	}
}

// Advisor note on FR-022: a per-turn block on the native model costs minutes
// of uncached prefill, so the combination is detected (and warned about).
func TestRetrievedOnNativeModel(t *testing.T) {
	native := &AgentInstance{Candidates: []providers.FallbackCandidate{{Provider: "native", Model: "Bonsai-1.7B-Q1_0"}}}
	external := &AgentInstance{Candidates: []providers.FallbackCandidate{{Provider: "ollama", Model: "gemma"}}}
	if !retrievedOnNativeModel(config.FocusMemoryRetrieved, native) {
		t.Fatal("retrieved on a native-first agent not detected")
	}
	if retrievedOnNativeModel(config.FocusMemoryRetrieved, external) {
		t.Fatal("external-first agent flagged as native")
	}
	if retrievedOnNativeModel(config.FocusMemoryCore, native) {
		t.Fatal("non-retrieved mode flagged")
	}
	if retrievedOnNativeModel(config.FocusMemoryRetrieved, nil) {
		t.Fatal("nil agent flagged")
	}
}

// A full turn in a retrieved window: the provider sees the per-turn memory
// block, but session history (and the seahorse ingest fed from the same
// message) keeps only what the user typed -- otherwise every turn would write
// memory into history and later retrievals would feed on their own output.
func TestRetrievedMemory_TurnNeverPersistsTheMemoryBlock(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "memory", "MEMORY.md"), []byte(retrievedMemoryMD), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         ws,
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
				Focus: config.FocusConfig{
					Enabled: true,
					Default: "recall",
					Windows: map[string]config.FocusWindow{"recall": {Memory: config.FocusMemoryRetrieved}},
				},
			},
		},
	}
	provider := &recordingProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	defer al.Close()

	msg := bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "telegram:123",
		ChatID:   "chat-1",
		Content:  "onde fica o rack?",
	}
	if _, err := al.processMessage(context.Background(), msg); err != nil {
		t.Fatalf("processMessage: %v", err)
	}

	sent := provider.lastMessages[len(provider.lastMessages)-1].Content
	if !strings.Contains(sent, retrievedBlockHeader) || !strings.Contains(sent, "o rack fica na sala") {
		t.Fatalf("provider did not receive the retrieved block (mode not applied?):\n%s", sent)
	}

	route, _, err := al.resolveMessageRoute(msg)
	if err != nil {
		t.Fatal(err)
	}
	sessionKey := resolveScopeKey(al.allocateRouteSession(route, msg).SessionKey, msg.SessionKey)
	history := al.GetRegistry().GetDefaultAgent().Sessions.GetHistory(sessionKey)
	var userTurns int
	for _, m := range history {
		if strings.Contains(m.Content, retrievedBlockHeader) {
			t.Fatalf("memory block persisted in session history: %q", m.Content)
		}
		if m.Role == "user" {
			userTurns++
			if m.Content != "onde fica o rack?" {
				t.Fatalf("persisted user message = %q, want the original text", m.Content)
			}
		}
	}
	if userTurns != 1 {
		t.Fatalf("persisted user turns = %d, want 1", userTurns)
	}
}

// Each floor change keys a new cached variant; the stale one for the same
// request shape must be dropped, not kept for the life of the process.
func TestRetrievedMemory_FloorChangeReplacesCachedVariant(t *testing.T) {
	cb, ws := newRetrievedBuilder(t, retrievedMemoryMD)
	cb.buildSystemPromptForRequest(retrievedRequest("oi"))
	for i, rule := range []string{"nunca reinicie o servidor de dia", "nunca desligue o nobreak"} {
		other, err := refinery.OpenStore(filepath.Join(ws, "memory", "atoms.db"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := other.Add(context.Background(), "", rule, refinery.FlagOriginImport); err != nil {
			t.Fatal(err)
		}
		other.Close()
		cb.buildSystemPromptForRequest(retrievedRequest("oi"))
		cb.systemPromptMutex.RLock()
		n := 0
		for key := range cb.cachedVariants {
			if strings.Contains(key, "|floor=") {
				n++
			}
		}
		cb.systemPromptMutex.RUnlock()
		if n != 1 {
			t.Fatalf("after floor change %d: %d retrieved variants cached, want 1", i+1, n)
		}
	}
}
