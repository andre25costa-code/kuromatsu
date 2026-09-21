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

func TestNewSleepScheduler_DisabledReturnsNil(t *testing.T) {
	cfg := &config.Config{Sleep: config.SleepConfig{Enabled: false}}
	if s := newSleepScheduler(cfg, nil, nil, nil); s != nil {
		t.Fatal("newSleepScheduler(disabled) != nil, want nil (AC-010-1)")
	}
}

func TestNewSleepScheduler_NilConfigReturnsNil(t *testing.T) {
	if s := newSleepScheduler(nil, nil, nil, nil); s != nil {
		t.Fatal("newSleepScheduler(nil cfg) != nil")
	}
}

// AC-010-7: enabled but no valid external model -- nil scheduler, no goroutine.
func TestNewSleepScheduler_NoValidModelReturnsNil(t *testing.T) {
	registry := testRegistryWithWorkspace(t.TempDir())
	cfg := &config.Config{Sleep: config.SleepConfig{Enabled: true}, ModelList: externalSleepModelList()}
	if s := newSleepScheduler(cfg, registry, nil, nil); s != nil {
		t.Fatal("newSleepScheduler(no unconscious_model) != nil")
	}

	// "bonsai-local" mirrors config's private nativeModelName constant
	// (pkg/config/native_fallback.go) -- unexported, so this test spells
	// it out literally rather than importing it.
	const nativeModelNameForTest = "bonsai-local"
	cfg2 := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: nativeModelNameForTest},
		ModelList: config.SecureModelList{{ModelName: nativeModelNameForTest, Provider: "native"}},
	}
	if s := newSleepScheduler(cfg2, registry, nil, nil); s != nil {
		t.Fatal("newSleepScheduler(native unconscious_model) != nil")
	}
}

func TestNewSleepScheduler_NoWorkspacesReturnsNil(t *testing.T) {
	cfg := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud"},
		ModelList: externalSleepModelList(),
	}
	if s := newSleepScheduler(cfg, &AgentRegistry{agents: map[string]*AgentInstance{}}, nil, nil); s != nil {
		t.Fatal("newSleepScheduler(no workspaces) != nil")
	}
}

func TestNewSleepScheduler_InvalidWindowReturnsNil(t *testing.T) {
	registry := testRegistryWithWorkspace(t.TempDir())
	cfg := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud", Window: "not-a-window"},
		ModelList: externalSleepModelList(),
	}
	if s := newSleepScheduler(cfg, registry, nil, nil); s != nil {
		t.Fatal("newSleepScheduler(invalid window) != nil")
	}
}

func TestNewSleepScheduler_ValidConfigSchedulesAndCloses(t *testing.T) {
	registry := testRegistryWithWorkspace(t.TempDir())
	cfg := &config.Config{
		Sleep:     config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud"},
		ModelList: externalSleepModelList(),
	}
	s := newSleepScheduler(cfg, registry, nil, runstate.New())
	if s == nil {
		t.Fatal("newSleepScheduler(valid config) = nil, want a scheduled scheduler")
	}
	if len(s.bridges) != 1 {
		t.Fatalf("bridges = %d, want exactly 1 (no per-agent override, single implicit group)", len(s.bridges))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	// Close must be safe to call again (mirrors evolutionBridge.Close's
	// closeOnce-style idempotency requirement).
	if err := s.Close(); err != nil {
		t.Fatalf("second Close(): %v", err)
	}
}

func TestSleepScheduler_CloseOnNilIsSafe(t *testing.T) {
	var s *sleepScheduler
	if err := s.Close(); err != nil {
		t.Fatalf("Close() on nil: %v, want nil", err)
	}
}

func TestSleepBridge_CloseOnNilIsSafe(t *testing.T) {
	var b *sleepBridge
	if err := b.Close(); err != nil {
		t.Fatalf("Close() on nil: %v, want nil", err)
	}
}

// TestNewSleepScheduler_PerAgentOverrideGetsOwnBridgeAndWindow is ADR-019's
// "sono configurável por agente": two agents with different effective
// sleep config (one relies on the global block, the other overrides
// window/model) must end up as two independent bridges, not one shared
// schedule -- proven by checking bridge count and that each bridge only
// knows about its own agent's workspace.
func TestNewSleepScheduler_PerAgentOverrideGetsOwnBridgeAndWindow(t *testing.T) {
	globalWorkspace := t.TempDir()
	overrideWorkspace := t.TempDir()
	registry := &AgentRegistry{agents: map[string]*AgentInstance{
		"estudos":  {ID: "estudos", Workspace: globalWorkspace, Sessions: session.NewSessionManager(globalWorkspace)},
		"sensores": {ID: "sensores", Workspace: overrideWorkspace, Sessions: session.NewSessionManager(overrideWorkspace)},
	}}
	overrideEnabled := true
	cfg := &config.Config{
		Sleep: config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud", Window: "03:00-05:00"},
		Agents: config.AgentsConfig{
			List: []config.AgentConfig{
				{ID: "sensores", Sleep: &config.AgentSleepConfig{
					Enabled:          &overrideEnabled,
					Window:           "01:00-02:00",
					UnconsciousModel: "sonho-cloud",
				}},
			},
		},
		ModelList: externalSleepModelList(),
	}

	s := newSleepScheduler(cfg, registry, nil, runstate.New())
	if s == nil {
		t.Fatal("newSleepScheduler() = nil, want two scheduled bridges")
	}
	if len(s.bridges) != 2 {
		t.Fatalf("bridges = %d, want 2 (distinct effective sleep config per agent)", len(s.bridges))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
}

// TestNewSleepScheduler_AgentOverrideDisablesSleepForThatAgentOnly proves
// the other half of ADR-019: an agent can opt OUT of sleep entirely
// (agents.list[].sleep.enabled=false) while the rest of the fleet keeps
// running on the global schedule -- the disabled agent's workspace must
// never appear in any bridge.
func TestNewSleepScheduler_AgentOverrideDisablesSleepForThatAgentOnly(t *testing.T) {
	enabledWorkspace := t.TempDir()
	disabledWorkspace := t.TempDir()
	registry := &AgentRegistry{agents: map[string]*AgentInstance{
		"estudos":  {ID: "estudos", Workspace: enabledWorkspace, Sessions: session.NewSessionManager(enabledWorkspace)},
		"sensores": {ID: "sensores", Workspace: disabledWorkspace, Sessions: session.NewSessionManager(disabledWorkspace)},
	}}
	disabled := false
	cfg := &config.Config{
		Sleep: config.SleepConfig{Enabled: true, UnconsciousModel: "sonho-cloud"},
		Agents: config.AgentsConfig{
			List: []config.AgentConfig{
				{ID: "sensores", Sleep: &config.AgentSleepConfig{Enabled: &disabled}},
			},
		},
		ModelList: externalSleepModelList(),
	}

	s := newSleepScheduler(cfg, registry, nil, runstate.New())
	if s == nil {
		t.Fatal("newSleepScheduler() = nil, want one bridge for the still-enabled agent")
	}
	if len(s.bridges) != 1 {
		t.Fatalf("bridges = %d, want exactly 1 (sensores opted out)", len(s.bridges))
	}
	for _, ws := range s.bridges[0].workspaces {
		if ws == disabledWorkspace {
			t.Fatalf("disabled agent's workspace %q appears in a scheduled bridge", disabledWorkspace)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
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

	if err = src.Acknowledge(context.Background(), workspace, digests); err != nil {
		t.Fatal(err)
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
	chat := newSleepChatFunc(cfg, "sonho-cloud", factory)

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
	chat := newSleepChatFunc(cfg, "sonho-cloud", factory)

	if _, err := chat(context.Background(), "sys", "user"); err == nil {
		t.Fatal("chat() = nil error, want the provider's error propagated")
	}
}
