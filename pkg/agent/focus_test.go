package agent

import (
	"context"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

// namedNoopTool is a minimal Tool for asserting exactly which tool names a
// window resolves to, without depending on the real file/exec/sysmon tool
// constructors (config wiring, workspace paths, etc).
type namedNoopTool struct{ name string }

func (t *namedNoopTool) Name() string        { return t.name }
func (t *namedNoopTool) Description() string { return "test tool " + t.name }
func (t *namedNoopTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *namedNoopTool) Execute(_ context.Context, _ map[string]any) *tools.ToolResult {
	return tools.SilentResult("ok")
}

func registerNamedTools(al *AgentLoop, names ...string) {
	for _, name := range names {
		al.RegisterTool(&namedNoopTool{name: name})
	}
}

func toolNamesOf(defs []providers.ToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	sort.Strings(names)
	return names
}

// TestFocus_DisabledIsByteIdenticalToPreFocusBehavior is the AC-014-9
// compatibility regression: with focus.enabled=false (the zero value —
// nothing new configured at all), every tool stays visible and the system
// prompt is the full (non-compact) identity, exactly like before A1-A5
// existed.
func TestFocus_DisabledIsByteIdenticalToPreFocusBehavior(t *testing.T) {
	cfg := &config.Config{}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	registerNamedTools(al, "read_file", "exec", "sysmon")
	agent := al.GetRegistry().GetDefaultAgent()

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "agent:default:test-focus-disabled",
		UserMessage:     "arquivo comando exec pesquise",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	// newTurnProfileAgentLoop builds a real *AgentLoop (NewAgentLoop), which
	// also registers the default-enabled shared tools (e.g. "reaction") that
	// need no workspace/exec wiring -- so the visible set is a superset of
	// the 3 named test tools, not exactly those 3. AC-014-9 only requires
	// that focus-disabled hides nothing; it says nothing about what else a
	// real agent loop registers on its own.
	got := toolNamesOf(provider.tools)
	for _, want := range []string{"read_file", "exec", "sysmon"} {
		if !slices.Contains(got, want) {
			t.Fatalf("tools = %v, want %q visible (focus disabled)", got, want)
		}
	}
	if strings.Contains(provider.messages[0].Content, "# kuromatsu\nWorkspace:") {
		t.Fatalf("system prompt looks compact despite focus disabled:\n%s", provider.messages[0].Content)
	}
}

func defaultFocusChatCfg() *config.Config {
	return &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Focus: config.FocusConfig{Enabled: true},
			},
		},
	}
}

// TestFocus_ChatWindowZeroToolsAndCompactIdentity covers the "chat" row of
// the ADR-014/S16 default window table: no trigger matches -> Default
// ("chat") -> 0 tools, compact identity.
func TestFocus_ChatWindowZeroToolsAndCompactIdentity(t *testing.T) {
	cfg := defaultFocusChatCfg()
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	registerNamedTools(al, "read_file", "exec", "sysmon")
	agent := al.GetRegistry().GetDefaultAgent()

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "agent:default:test-focus-chat",
		UserMessage:     "bom dia",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 0 {
		t.Fatalf("chat window tools = %#v, want 0", provider.tools)
	}
	if !strings.Contains(provider.messages[0].Content, "# kuromatsu\nWorkspace:") {
		t.Fatalf("chat window system prompt not compact:\n%s", provider.messages[0].Content)
	}
}

// TestFocus_FilesWindowExactlyFiveTools covers the "files" row: a message
// matching the files keyword trigger resolves to exactly the 5 file tools.
func TestFocus_FilesWindowExactlyFiveTools(t *testing.T) {
	cfg := defaultFocusChatCfg()
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	registerNamedTools(al, "read_file", "list_dir", "write_file", "edit_file", "append_file", "exec", "sysmon")
	agent := al.GetRegistry().GetDefaultAgent()

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "agent:default:test-focus-files",
		UserMessage:     "leia o arquivo README.md",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 5 {
		t.Fatalf("files window tools = %#v, want exactly 5", provider.tools)
	}
	got := toolNamesOf(provider.tools)
	want := []string{"append_file", "edit_file", "list_dir", "read_file", "write_file"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files window tool names = %v, want %v", got, want)
	}
}

// TestFocus_HeartbeatOriginUsesOriginWindowNotKeywordRules covers AC-014-3:
// heartbeat's window comes from Origins, never from the message content
// (even when the heartbeat prompt happens to contain a "files" trigger
// word).
func TestFocus_HeartbeatOriginUsesOriginWindowNotKeywordRules(t *testing.T) {
	cfg := defaultFocusChatCfg()
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	registerNamedTools(al, "message", "sysmon", "cron", "read_file")

	_, err := al.ProcessHeartbeat(context.Background(), "leia o arquivo de status", "", "")
	if err != nil {
		t.Fatalf("ProcessHeartbeat() error = %v", err)
	}
	got := toolNamesOf(provider.tools)
	want := []string{"cron", "message", "sysmon"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("heartbeat window tools = %v, want %v (origin, not the files keyword rule)", got, want)
	}
}

// TestFocus_InlineTagRoutesAndStripsFromPersistedMessage covers AC-014-2:
// "[foco:files] ..." routes to files and the tag never reaches the
// persisted session history.
func TestFocus_InlineTagRoutesAndStripsFromPersistedMessage(t *testing.T) {
	cfg := defaultFocusChatCfg()
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	registerNamedTools(al, "read_file", "list_dir", "write_file", "edit_file", "append_file")
	agent := al.GetRegistry().GetDefaultAgent()

	sessionKey := "agent:default:test-focus-inline-tag"
	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      sessionKey,
		UserMessage:     "[foco:files] veja isso",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 5 {
		t.Fatalf("tools = %#v, want the 5 file tools (inline tag routed to files)", provider.tools)
	}
	history := agent.Sessions.GetHistory(sessionKey)
	if len(history) == 0 {
		t.Fatal("no history persisted")
	}
	if strings.Contains(history[0].Content, "[foco:") {
		t.Fatalf("persisted message still contains the inline tag: %q", history[0].Content)
	}
	if history[0].Content != "veja isso" {
		t.Fatalf("persisted message = %q, want tag stripped to \"veja isso\"", history[0].Content)
	}
}

// TestFocus_StickyAderenciaKeepsWindowForConfiguredTurns covers ADR-014
// point 1's aderência: after routing to "files" (sticky_turns=3 by
// default), a follow-up message with no signal at all stays on "files".
func TestFocus_StickyAderenciaKeepsWindowForConfiguredTurns(t *testing.T) {
	cfg := defaultFocusChatCfg()
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	registerNamedTools(al, "read_file", "list_dir", "write_file", "edit_file", "append_file")
	agent := al.GetRegistry().GetDefaultAgent()
	sessionKey := "agent:default:test-focus-sticky"

	if _, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      sessionKey,
		UserMessage:     "abra o arquivo x",
		DefaultResponse: defaultResponse,
	}); err != nil {
		t.Fatalf("first runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 5 {
		t.Fatalf("first turn tools = %#v, want files (5 tools)", provider.tools)
	}

	if _, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      sessionKey,
		UserMessage:     "e o outro tambem",
		DefaultResponse: defaultResponse,
	}); err != nil {
		t.Fatalf("second runAgentLoop() error = %v", err)
	}
	if len(provider.tools) != 5 {
		t.Fatalf("second turn tools = %#v, want files still sticky (5 tools)", provider.tools)
	}
}

type focusEscalationProvider struct {
	calls        int
	toolsPerCall [][]providers.ToolDefinition
}

func (p *focusEscalationProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	toolDefs []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.calls++
	p.toolsPerCall = append(p.toolsPerCall, append([]providers.ToolDefinition(nil), toolDefs...))
	if p.calls == 1 {
		return &providers.LLMResponse{
			Content: "calling exec",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "exec",
				Arguments: map[string]any{"action": "run", "command": "echo hi"},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	return &providers.LLMResponse{Content: "done, escalated"}, nil
}

func (p *focusEscalationProvider) GetDefaultModel() string { return "test-model" }

// TestFocus_EscalationRetriesWithFullWindowAndDiscardsDeniedCall covers
// AC-014-6: a tool that exists globally but is outside "chat" (which runs
// with tools off) escalates to "full" (chat's default EscalateTo) and
// retries — the denied first response is never persisted.
func TestFocus_EscalationRetriesWithFullWindowAndDiscardsDeniedCall(t *testing.T) {
	cfg := defaultFocusChatCfg()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	provider := &focusEscalationProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	registerNamedTools(al, "exec")
	agent := al.GetRegistry().GetDefaultAgent()
	sessionKey := "agent:default:test-focus-escalation"

	response, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      sessionKey,
		UserMessage:     "faz um exec pra mim",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if response != "done, escalated" {
		t.Fatalf("response = %q, want the second (escalated) call's content", response)
	}
	if provider.calls != 2 {
		t.Fatalf("provider.calls = %d, want 2 (one retry after escalation)", provider.calls)
	}
	if len(provider.toolsPerCall[0]) != 0 {
		t.Fatalf("first call tools = %#v, want 0 (chat window)", provider.toolsPerCall[0])
	}
	// "full" is the unrestricted ceiling -- every registered tool is visible
	// there, not just "exec" (NewAgentLoop also registers default-enabled
	// shared tools like "reaction"). The escalation contract is that "exec"
	// becomes reachable, not that it becomes the *only* reachable tool.
	if !slices.Contains(toolNamesOf(provider.toolsPerCall[1]), "exec") {
		t.Fatalf("second call tools = %#v, want \"exec\" reachable (escalated to full)", provider.toolsPerCall[1])
	}

	history := agent.Sessions.GetHistory(sessionKey)
	for _, msg := range history {
		if msg.Role == "assistant" && msg.Content == "calling exec" {
			t.Fatalf("first (denied) response was persisted to history despite escalation discarding it: %#v", history)
		}
	}
}

type focusEscalationCapProvider struct {
	calls        int
	toolsPerCall [][]providers.ToolDefinition
}

func (p *focusEscalationCapProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	toolDefs []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.calls++
	p.toolsPerCall = append(p.toolsPerCall, append([]providers.ToolDefinition(nil), toolDefs...))
	switch p.calls {
	case 1:
		// Turn starts in "chat" (tools off) and asks for "sysmon", a tool
		// that exists globally but is outside chat's window: this escalates
		// (count 0 -> 1) to chat's configured EscalateTo ("shell" here,
		// deliberately not "full" — see the test doc comment for why).
		return &providers.LLMResponse{
			Content: "call sysmon from chat",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_1",
				Name: "sysmon",
			}},
			FinishReason: "tool_calls",
		}, nil
	case 2:
		// Now in "shell" (allows only "exec"). Asking for "sysmon" again is
		// still outside the active window, but MaxEscalationsPerTurn=1 was
		// already spent on the first call — this must NOT escalate again.
		return &providers.LLMResponse{
			Content: "call sysmon again from shell",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_2",
				Name: "sysmon",
			}},
			FinishReason: "tool_calls",
		}, nil
	default:
		return &providers.LLMResponse{Content: "done after cap"}, nil
	}
}

func (p *focusEscalationCapProvider) GetDefaultModel() string { return "test-model" }

// TestFocus_EscalationCapDeniesSecondOutOfWindowToolInSameTurn covers
// AC-014-7: once a turn has used its MaxEscalationsPerTurn budget (default
// 1), a second out-of-window tool call in the same turn must be denied by
// denyByTurnProfile (pipeline_execute.go) instead of escalating again.
//
// The test deliberately configures chat's EscalateTo as "shell" (itself
// still tool-restricted to "exec") rather than the real default "full":
// escalating straight to "full" would make every tool allowed from the
// second call onward, which would make a *second* escalation attempt
// unobservable (nothing would ever be outside "full"'s window again). Using
// a still-restricted escalation target is what makes the cap's effect
// (a second denial, not a second escalation) directly verifiable.
func TestFocus_EscalationCapDeniesSecondOutOfWindowToolInSameTurn(t *testing.T) {
	shellWindow := "shell"
	fullWindow := "full"
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Focus: config.FocusConfig{
					Enabled: true,
					Windows: map[string]config.FocusWindow{
						"chat": {
							Tools:      config.TurnProfileBlock{Mode: config.TurnProfileModeOff},
							EscalateTo: &shellWindow,
						},
						"shell": {
							Tools: config.TurnProfileBlock{
								Mode:  config.TurnProfileModeCustom,
								Allow: []string{"exec"},
							},
							EscalateTo: &fullWindow,
						},
					},
				},
			},
		},
	}
	cfg.Agents.Defaults.Workspace = t.TempDir()
	provider := &focusEscalationCapProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	registerNamedTools(al, "exec", "sysmon")
	agent := al.GetRegistry().GetDefaultAgent()
	sessionKey := "agent:default:test-focus-escalation-cap"

	response, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      sessionKey,
		UserMessage:     "oi",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if response != "done after cap" {
		t.Fatalf("response = %q, want the third call's content", response)
	}
	if provider.calls != 3 {
		t.Fatalf(
			"provider.calls = %d, want 3 (escalate once, deny the second out-of-window call, then finalize)",
			provider.calls,
		)
	}
	if len(provider.toolsPerCall[0]) != 0 {
		t.Fatalf("first call tools = %#v, want 0 (chat window)", provider.toolsPerCall[0])
	}
	wantShellTools := []string{"exec"}
	for i, label := range []string{"second", "third"} {
		got := toolNamesOf(provider.toolsPerCall[i+1])
		if strings.Join(got, ",") != strings.Join(wantShellTools, ",") {
			t.Fatalf(
				"%s call tools = %v, want %v (still shell window: the cap denied a second escalation)",
				label, got, wantShellTools,
			)
		}
	}

	history := agent.Sessions.GetHistory(sessionKey)
	var deniedCount int
	for _, msg := range history {
		if msg.Role == "tool" && strings.Contains(msg.Content, "not allowed by the active turn profile") {
			deniedCount++
		}
	}
	if deniedCount != 1 {
		t.Fatalf(
			"denied-tool messages in history = %d, want exactly 1 (the second sysmon call denied instead of escalating again)",
			deniedCount,
		)
	}
}

// TestFocus_UnknownToolGetsHintWithoutEscalating covers AC-014-5: a tool
// name that doesn't exist anywhere in the global registry never escalates
// (no second LLM call over it) — it gets rejected immediately with a hint.
func TestFocus_UnknownToolGetsHintWithoutEscalating(t *testing.T) {
	cfg := defaultFocusChatCfg()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	provider := &turnProfileToolCallProvider{} // 1st call: echo_text_rewritten tool call; 2nd: "done"
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	registerNamedTools(al, "echo_text") // "echo_text_rewritten" (requested) is NOT registered at all
	agent := al.GetRegistry().GetDefaultAgent()

	response, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "agent:default:test-focus-unknown-tool",
		UserMessage:     "faz uma coisa",
		DefaultResponse: defaultResponse,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}
	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	if provider.calls != 2 {
		t.Fatalf("provider.calls = %d, want 2 (no escalation, straight to the tool-not-found retry)", provider.calls)
	}
	var foundHint bool
	for _, msg := range provider.messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "não existe") {
			foundHint = true
			if !strings.Contains(msg.Content, "echo_text") {
				t.Fatalf("hint missing the available-tools list: %q", msg.Content)
			}
		}
	}
	if !foundHint {
		t.Fatalf("no unknown-tool hint found in second call's messages: %#v", provider.messages)
	}
}
