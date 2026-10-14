package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

// repeatCountingProvider wraps a provider and counts Chat calls.
type repeatCountingProvider struct {
	providers.LLMProvider
	calls atomic.Int32
}

func (p *repeatCountingProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	toolDefs []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	p.calls.Add(1)
	return p.LLMProvider.Chat(ctx, messages, toolDefs, model, opts)
}

// repeatCountingTool wraps a tool and counts executions.
type repeatCountingTool struct {
	tools.Tool
	calls atomic.Int32
}

func (t *repeatCountingTool) Execute(ctx context.Context, args map[string]any) *tools.ToolResult {
	t.calls.Add(1)
	return t.Tool.Execute(ctx, args)
}

// repeatScriptedProvider answers call n (1-based) with script(n).
type repeatScriptedProvider struct {
	mu     sync.Mutex
	n      int
	script func(n int) *providers.LLMResponse
}

func (p *repeatScriptedProvider) Chat(
	context.Context, []providers.Message, []providers.ToolDefinition, string, map[string]any,
) (*providers.LLMResponse, error) {
	p.mu.Lock()
	p.n++
	n := p.n
	p.mu.Unlock()
	return p.script(n), nil
}

func (p *repeatScriptedProvider) GetDefaultModel() string { return "repeat-scripted-model" }

func (p *repeatScriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

// repeatGuardTool is a tool whose result is result(n) for its n-th call.
type repeatGuardTool struct {
	name   string
	calls  atomic.Int32
	result func(n int) string
}

func (t *repeatGuardTool) Name() string        { return t.name }
func (t *repeatGuardTool) Description() string { return "tool used by the repeated-call guard tests" }
func (t *repeatGuardTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"value": map[string]any{"type": "string"}},
	}
}

func (t *repeatGuardTool) Execute(context.Context, map[string]any) *tools.ToolResult {
	n := int(t.calls.Add(1))
	return tools.SilentResult(t.result(n))
}

func constantResult(s string) func(int) string {
	return func(int) string { return s }
}

func toolCallResponse(calls ...providers.ToolCall) *providers.LLMResponse {
	return &providers.LLMResponse{ToolCalls: calls}
}

func repeatToolCall(id, name string, args map[string]any) providers.ToolCall {
	return providers.ToolCall{ID: id, Type: "function", Name: name, Arguments: args}
}

func newRepeatGuardLoop(t *testing.T, provider providers.LLMProvider, limit, maxIterations int) *AgentLoop {
	t.Helper()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:            t.TempDir(),
				ModelName:            "test-model",
				MaxTokens:            4096,
				MaxToolIterations:    maxIterations,
				MaxRepeatedToolCalls: limit,
			},
		},
	}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	t.Cleanup(al.Close)
	return al
}

func repeatGuardHistory(t *testing.T, al *AgentLoop) []providers.Message {
	t.Helper()
	defaultAgent := al.registry.GetDefaultAgent()
	if defaultAgent == nil {
		t.Fatal("no default agent")
	}
	route := al.registry.ResolveRoute(bus.InboundContext{Channel: "test", ChatType: "direct", SenderID: "cli"})
	key := al.allocateRouteSession(route, testInboundMessage(bus.InboundMessage{
		Channel:  "test",
		SenderID: "cli",
		ChatID:   "chat1",
	})).SessionKey
	return defaultAgent.Sessions.GetHistory(key)
}

// assertEveryToolCallAnswered checks that each assistant tool_call is
// followed by a role="tool" message with its ToolCallID.
func assertEveryToolCallAnswered(t *testing.T, history []providers.Message) {
	t.Helper()
	for i, msg := range history {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for j, call := range msg.ToolCalls {
			k := i + 1 + j
			if k >= len(history) || history[k].Role != "tool" || history[k].ToolCallID != call.ID {
				t.Fatalf("tool_call %q at history[%d] has no matching tool message at history[%d]", call.ID, i, k)
			}
		}
	}
}

func countToolMessagesContaining(history []providers.Message, substr string) int {
	n := 0
	for _, msg := range history {
		if msg.Role == "tool" && strings.Contains(msg.Content, substr) {
			n++
		}
	}
	return n
}

func runRepeatGuardTurn(t *testing.T, al *AgentLoop) string {
	t.Helper()
	response, err := al.ProcessDirectWithChannel(context.Background(), "hello", "repeat-guard", "test", "chat1")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel: %v", err)
	}
	return response
}

func TestRepeatGuard_WarnsThenStopsTurn(t *testing.T) {
	provider := &repeatCountingProvider{LLMProvider: &toolLimitOnlyProvider{}}
	al := newRepeatGuardLoop(t, provider, 3, 10)
	tool := &repeatCountingTool{Tool: &toolLimitTestTool{}}
	al.RegisterTool(tool)

	response := runRepeatGuardTurn(t, al)

	want := fmt.Sprintf(repeatedToolCallResponse, "tool_limit_test_tool")
	if response != want {
		t.Fatalf("response = %q, want %q", response, want)
	}
	if got := tool.calls.Load(); got != 2 {
		t.Fatalf("tool executed %d times, want 2", got)
	}
	if got := provider.calls.Load(); got != 4 {
		t.Fatalf("provider called %d times, want 4", got)
	}

	history := repeatGuardHistory(t, al)
	assertEveryToolCallAnswered(t, history)
	warning := fmt.Sprintf(repeatedToolCallWarning, "tool_limit_test_tool", 2)
	if got := countToolMessagesContaining(history, warning); got != 1 {
		t.Fatalf("warning tool messages = %d, want 1", got)
	}
	if got := countToolMessagesContaining(history, repeatedToolCallSkipMessage); got != 1 {
		t.Fatalf("skip tool messages = %d, want 1", got)
	}
	if last := history[len(history)-1]; last.Role != "assistant" || last.Content != want {
		t.Fatalf("last history message = %s %q, want the stop message", last.Role, last.Content)
	}
}

func TestRepeatGuard_DisabledKeepsToolLimitBehavior(t *testing.T) {
	provider := &repeatCountingProvider{LLMProvider: &toolLimitOnlyProvider{}}
	al := newRepeatGuardLoop(t, provider, 0, 10)
	tool := &repeatCountingTool{Tool: &toolLimitTestTool{}}
	al.RegisterTool(tool)

	response := runRepeatGuardTurn(t, al)

	if response != toolLimitResponse {
		t.Fatalf("response = %q, want %q", response, toolLimitResponse)
	}
	if got := provider.calls.Load(); got != 10 {
		t.Fatalf("provider called %d times, want 10", got)
	}
	if got := tool.calls.Load(); got != 10 {
		t.Fatalf("tool executed %d times, want 10", got)
	}
}

func TestRepeatGuard_PollingWithChangingOutputNotBlocked(t *testing.T) {
	provider := &repeatScriptedProvider{script: func(n int) *providers.LLMResponse {
		if n <= 6 {
			call := repeatToolCall(fmt.Sprintf("call_%d", n), "poll_tool", map[string]any{"value": "x"})
			return toolCallResponse(call)
		}
		return &providers.LLMResponse{Content: "done"}
	}}
	al := newRepeatGuardLoop(t, provider, 3, 20)
	tool := &repeatGuardTool{name: "poll_tool", result: func(n int) string { return fmt.Sprintf("n=%d", n) }}
	al.RegisterTool(tool)

	response := runRepeatGuardTurn(t, al)

	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	if got := tool.calls.Load(); got != 6 {
		t.Fatalf("tool executed %d times, want 6", got)
	}
	history := repeatGuardHistory(t, al)
	if got := countToolMessagesContaining(history, "Repeated call blocked"); got != 0 {
		t.Fatalf("warning tool messages = %d, want 0", got)
	}
}

func TestRepeatGuard_AlternatingCallsNotBlocked(t *testing.T) {
	provider := &repeatScriptedProvider{script: func(n int) *providers.LLMResponse {
		if n > 6 {
			return &providers.LLMResponse{Content: "done"}
		}
		name := "tool_a"
		if n%2 == 0 {
			name = "tool_b"
		}
		return toolCallResponse(repeatToolCall(fmt.Sprintf("call_%d", n), name, map[string]any{"value": "x"}))
	}}
	al := newRepeatGuardLoop(t, provider, 2, 20)
	toolA := &repeatGuardTool{name: "tool_a", result: constantResult("a")}
	toolB := &repeatGuardTool{name: "tool_b", result: constantResult("b")}
	al.RegisterTool(toolA)
	al.RegisterTool(toolB)

	response := runRepeatGuardTurn(t, al)

	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	if a, b := toolA.calls.Load(), toolB.calls.Load(); a != 3 || b != 3 {
		t.Fatalf("executions a=%d b=%d, want 3 and 3", a, b)
	}
	if got := countToolMessagesContaining(repeatGuardHistory(t, al), "Repeated call blocked"); got != 0 {
		t.Fatalf("warning tool messages = %d, want 0", got)
	}
}

func TestRepeatGuard_DifferentArgsNotBlocked(t *testing.T) {
	provider := &repeatScriptedProvider{script: func(n int) *providers.LLMResponse {
		if n > 6 {
			return &providers.LLMResponse{Content: "done"}
		}
		return toolCallResponse(repeatToolCall(
			fmt.Sprintf("call_%d", n), "args_tool", map[string]any{"value": fmt.Sprintf("v%d", n)},
		))
	}}
	al := newRepeatGuardLoop(t, provider, 2, 20)
	tool := &repeatGuardTool{name: "args_tool", result: constantResult("same")}
	al.RegisterTool(tool)

	response := runRepeatGuardTurn(t, al)

	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	if got := tool.calls.Load(); got != 6 {
		t.Fatalf("tool executed %d times, want 6", got)
	}
	if got := countToolMessagesContaining(repeatGuardHistory(t, al), "Repeated call blocked"); got != 0 {
		t.Fatalf("warning tool messages = %d, want 0", got)
	}
}

func TestRepeatGuard_IdenticalCallsInSameResponse(t *testing.T) {
	args := map[string]any{"value": "x"}
	provider := &repeatScriptedProvider{script: func(n int) *providers.LLMResponse {
		if n == 1 {
			return toolCallResponse(
				repeatToolCall("call_1", "same_tool", args),
				repeatToolCall("call_2", "same_tool", args),
				repeatToolCall("call_3", "same_tool", args),
			)
		}
		return &providers.LLMResponse{Content: "done"}
	}}
	al := newRepeatGuardLoop(t, provider, 3, 20)
	tool := &repeatGuardTool{name: "same_tool", result: constantResult("same")}
	al.RegisterTool(tool)

	response := runRepeatGuardTurn(t, al)

	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	if got := tool.calls.Load(); got != 2 {
		t.Fatalf("tool executed %d times, want 2", got)
	}
	if got := provider.callCount(); got != 2 {
		t.Fatalf("provider called %d times, want 2", got)
	}
	history := repeatGuardHistory(t, al)
	assertEveryToolCallAnswered(t, history)
	warning := fmt.Sprintf(repeatedToolCallWarning, "same_tool", 2)
	for _, msg := range history {
		if msg.Role == "tool" && msg.ToolCallID == "call_3" {
			if msg.Content != warning {
				t.Fatalf("call_3 result = %q, want the warning", msg.Content)
			}
			return
		}
	}
	t.Fatal("no tool message for call_3")
}

func TestRepeatGuard_HookRespondCountsTowardGuard(t *testing.T) {
	provider := &repeatCountingProvider{LLMProvider: &toolLimitOnlyProvider{}}
	al := newRepeatGuardLoop(t, provider, 3, 10)
	tool := &repeatCountingTool{Tool: &toolLimitTestTool{}}
	al.RegisterTool(tool)
	// The hook answers every call with the same result instead of the tool.
	if err := al.MountHook(NamedHook("respond-hook", &respondHook{
		respondTools: map[string]bool{"tool_limit_test_tool": true},
	})); err != nil {
		t.Fatalf("MountHook: %v", err)
	}

	response := runRepeatGuardTurn(t, al)

	want := fmt.Sprintf(repeatedToolCallResponse, "tool_limit_test_tool")
	if response != want {
		t.Fatalf("response = %q, want %q", response, want)
	}
	if got := tool.calls.Load(); got != 0 {
		t.Fatalf("tool executed %d times, want 0 (the hook answers)", got)
	}
	if got := provider.calls.Load(); got != 4 {
		t.Fatalf("provider called %d times, want 4", got)
	}
	history := repeatGuardHistory(t, al)
	assertEveryToolCallAnswered(t, history)
	if got := countToolMessagesContaining(history, "hook-responded: tool_limit_test_tool"); got != 2 {
		t.Fatalf("hook answers in history = %d, want 2", got)
	}
}
