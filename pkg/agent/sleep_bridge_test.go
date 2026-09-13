package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
	"github.com/andre25costa-code/kuromatsu/pkg/session"
)

func testRegistryWithWorkspace(workspace string) *AgentRegistry {
	store := session.NewSessionManager(workspace)
	return &AgentRegistry{
		agents: map[string]*AgentInstance{
			"default": {ID: "default", Workspace: workspace, Sessions: store},
		},
	}
}

func externalSleepModelList() config.SecureModelList {
	return config.SecureModelList{{ModelName: "sonho-cloud", Provider: "openai", Model: "gpt-5.4"}}
}

func TestNewSleepBridge_DisabledReturnsNil(t *testing.T) {
	cfg := &config.Config{Sleep: config.SleepConfig{Enabled: false}}
	if b := newSleepBridge(cfg, nil, nil, nil); b != nil {
		t.Fatal("newSleepBridge(disabled) != nil, want nil (AC-010-1)")
	}
}

func TestNewSleepBridge_NilConfigReturnsNil(t *testing.T) {
	if b := newSleepBridge(nil, nil, nil, nil); b != nil {
		t.Fatal("newSleepBridge(nil cfg) != nil")
	}
}

// AC-010-7: enabled but no valid external model -- nil bridge, no goroutine.
func TestNewSleepBridge_NoValidModelReturnsNil(t *testing.T) {
	registry := testRegistryWithWorkspace(t.TempDir())
	cfg := &config.Config{Sleep: config.SleepConfig{Enabled: true}, ModelList: externalSleepModelList()}
	if b := newSleepBridge(cfg, registry, nil, nil); b != nil {
		t.Fatal("newSleepBridge(no unconscious_model) != nil")
	}

	// "bonsai-local" mirrors config's private nativeModelName constant
	// (pkg/config/native_fallback.go) -- unexported, so this test spells
	// it out literally rather than importing it.
	const nativeModelNameForTest = "bonsai-local"
	cfg2 := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: nativeModelNameForTest},
		ModelList: config.SecureModelList{{ModelName: nativeModelNameForTest, Provider: "native"}},
	}
	if b := newSleepBridge(cfg2, registry, nil, nil); b != nil {
		t.Fatal("newSleepBridge(native unconscious_model) != nil")
	}
}

func TestNewSleepBridge_NoWorkspacesReturnsNil(t *testing.T) {
	cfg := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud"},
		ModelList: externalSleepModelList(),
	}
	if b := newSleepBridge(cfg, &AgentRegistry{agents: map[string]*AgentInstance{}}, nil, nil); b != nil {
		t.Fatal("newSleepBridge(no workspaces) != nil")
	}
}

func TestNewSleepBridge_InvalidWindowReturnsNil(t *testing.T) {
	registry := testRegistryWithWorkspace(t.TempDir())
	cfg := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud", Window: "not-a-window"},
		ModelList: externalSleepModelList(),
	}
	if b := newSleepBridge(cfg, registry, nil, nil); b != nil {
		t.Fatal("newSleepBridge(invalid window) != nil")
	}
}

func TestNewSleepBridge_ValidConfigSchedulesAndCloses(t *testing.T) {
	registry := testRegistryWithWorkspace(t.TempDir())
	cfg := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud"},
		ModelList: externalSleepModelList(),
	}
	b := newSleepBridge(cfg, registry, nil, runstate.New())
	if b == nil {
		t.Fatal("newSleepBridge(valid config) = nil, want a scheduled bridge")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	// Close must be safe to call again (mirrors evolutionBridge.Close's
	// closeOnce-style idempotency requirement).
	if err := b.Close(); err != nil {
		t.Fatalf("second Close(): %v", err)
	}
}

func TestSleepBridge_CloseOnNilIsSafe(t *testing.T) {
	var b *sleepBridge
	if err := b.Close(); err != nil {
		t.Fatalf("Close() on nil: %v, want nil", err)
	}
}

func TestAgentSessionSource_TracksMessageCountCursor(t *testing.T) {
	workspace := t.TempDir()
	store := session.NewSessionManager(workspace)
	store.AddMessage("s1", "user", "hello")
	store.AddMessage("s1", "assistant", "hi there")

	registry := &AgentRegistry{agents: map[string]*AgentInstance{
		"default": {ID: "default", Workspace: workspace, Sessions: store},
	}}
	src := newAgentSessionSource(registry)

	digests, err := src.RecentDigests(context.Background(), workspace, time.Time{}, false)
	if err != nil {
		t.Fatalf("RecentDigests: %v", err)
	}
	if len(digests) != 1 {
		t.Fatalf("digests = %d, want 1 on first call", len(digests))
	}

	digests2, err := src.RecentDigests(context.Background(), workspace, time.Time{}, false)
	if err != nil {
		t.Fatalf("RecentDigests: %v", err)
	}
	if len(digests2) != 0 {
		t.Fatalf("digests = %d, want 0 on second call with no new messages", len(digests2))
	}

	store.AddMessage("s1", "user", "one more thing")
	digests3, err := src.RecentDigests(context.Background(), workspace, time.Time{}, false)
	if err != nil {
		t.Fatalf("RecentDigests: %v", err)
	}
	if len(digests3) != 1 {
		t.Fatalf("digests = %d, want 1 after a new message was appended", len(digests3))
	}
}

func TestAgentSessionSource_WeeklyDeepIgnoresCursor(t *testing.T) {
	workspace := t.TempDir()
	store := session.NewSessionManager(workspace)
	store.AddMessage("s1", "user", "hello")

	registry := &AgentRegistry{agents: map[string]*AgentInstance{
		"default": {ID: "default", Workspace: workspace, Sessions: store},
	}}
	src := newAgentSessionSource(registry)
	if _, err := src.RecentDigests(context.Background(), workspace, time.Time{}, false); err != nil {
		t.Fatalf("RecentDigests: %v", err)
	}

	digests, err := src.RecentDigests(context.Background(), workspace, time.Time{}, true)
	if err != nil {
		t.Fatalf("RecentDigests: %v", err)
	}
	if len(digests) != 1 {
		t.Fatalf("weeklyDeep digests = %d, want 1 regardless of the cursor", len(digests))
	}
}

func TestAgentSessionSource_NoWorkspaceMatchReturnsEmpty(t *testing.T) {
	registry := &AgentRegistry{agents: map[string]*AgentInstance{}}
	src := newAgentSessionSource(registry)
	digests, err := src.RecentDigests(context.Background(), "nope", time.Time{}, false)
	if err != nil || len(digests) != 0 {
		t.Fatalf("RecentDigests(unknown workspace) = (%v, %v), want (empty, nil)", digests, err)
	}
}

func TestAgentSessionSource_EmptySummaryFallsBackToHistoryTail(t *testing.T) {
	workspace := t.TempDir()
	store := session.NewSessionManager(workspace)
	store.AddMessage("s1", "user", "what is the weather")
	store.AddMessage("s1", "assistant", "sunny")

	registry := &AgentRegistry{agents: map[string]*AgentInstance{
		"default": {ID: "default", Workspace: workspace, Sessions: store},
	}}
	src := newAgentSessionSource(registry)
	digests, err := src.RecentDigests(context.Background(), workspace, time.Time{}, false)
	if err != nil {
		t.Fatalf("RecentDigests: %v", err)
	}
	if len(digests) != 1 {
		t.Fatalf("digests = %d, want 1", len(digests))
	}
	if !strings.Contains(digests[0].Summary, "what is the weather") || !strings.Contains(digests[0].Summary, "sunny") {
		t.Fatalf("Summary = %q, want the rendered history tail", digests[0].Summary)
	}
}

type stubProviderForSleep struct {
	calledModel   string
	calledTools   []providers.ToolDefinition
	calledOptions map[string]any
	response      *providers.LLMResponse
	err           error
}

func (p *stubProviderForSleep) Chat(
	_ context.Context,
	_ []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	p.calledModel = model
	p.calledTools = tools
	p.calledOptions = options
	if p.err != nil {
		return nil, p.err
	}
	return p.response, nil
}

func (p *stubProviderForSleep) GetDefaultModel() string { return "stub" }

// AC-010-8: the sleep ChatFunc calls exclusively the configured candidate,
// with tools=nil, and reports back Text/TotalTokens from the response.
func TestNewSleepChatFunc_CallsConfiguredCandidateWithNilTools(t *testing.T) {
	stub := &stubProviderForSleep{
		response: &providers.LLMResponse{
			Content: "updated memory",
			Usage:   &providers.UsageInfo{TotalTokens: 42},
		},
	}
	cfg := &config.Config{
		Sleep:     config.SleepConfig{UnconsciousModel: "sonho-cloud"},
		ModelList: externalSleepModelList(),
	}
	factory := func(mc *config.ModelConfig) (providers.LLMProvider, string, error) {
		return stub, mc.ModelName, nil
	}
	chat := newSleepChatFunc(cfg, factory)

	result, err := chat(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("chat(): %v", err)
	}
	if result.Text != "updated memory" || result.TotalTokens != 42 {
		t.Fatalf("result = %+v, want Text=%q TotalTokens=42", result, "updated memory")
	}
	if stub.calledModel == "" {
		t.Fatal("provider.Chat was never called with a resolved model")
	}
	if stub.calledTools != nil {
		t.Fatalf("calledTools = %v, want nil (AC-010-8: tools=nil)", stub.calledTools)
	}
}

func TestNewSleepChatFunc_ProviderErrorPropagates(t *testing.T) {
	stub := &stubProviderForSleep{err: context.DeadlineExceeded}
	cfg := &config.Config{
		Sleep:     config.SleepConfig{UnconsciousModel: "sonho-cloud"},
		ModelList: externalSleepModelList(),
	}
	factory := func(mc *config.ModelConfig) (providers.LLMProvider, string, error) {
		return stub, mc.ModelName, nil
	}
	chat := newSleepChatFunc(cfg, factory)

	if _, err := chat(context.Background(), "sys", "user"); err == nil {
		t.Fatal("chat() = nil error, want the provider's error propagated")
	}
}
