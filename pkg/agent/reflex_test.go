package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

type fakeExecTool struct {
	lastArgs map[string]any
}

func (t *fakeExecTool) Name() string        { return "exec" }
func (t *fakeExecTool) Description() string { return "fake exec for reflex tests" }
func (t *fakeExecTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"action": map[string]any{"type": "string"}, "command": map[string]any{"type": "string"}},
	}
}
func (t *fakeExecTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	t.lastArgs = args
	cmd, _ := args["command"].(string)
	return tools.SilentResult("ran: " + cmd)
}

func inboundReflexMessage(content string) bus.InboundMessage {
	return bus.InboundMessage{
		Context: bus.InboundContext{
			Channel:  "pico",
			ChatID:   "pico:reflex-test",
			ChatType: "direct",
			SenderID: "pico-user",
		},
		Content: content,
		Sender:  bus.SenderInfo{DisplayName: "Andre"},
	}
}

// TestReflex_ReplyMatchesWithoutCallingLLM covers AC-013-1: a matched
// "reply" reflex renders its template and never touches the LLM.
func TestReflex_ReplyMatchesWithoutCallingLLM(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^oi$`, Action: config.ReflexActionReply, Template: "Oi, {{sender}}!"},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	response, err := al.processMessage(context.Background(), inboundReflexMessage("oi"))
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if response != "Oi, Andre!" {
		t.Fatalf("response = %q, want rendered template", response)
	}
	if len(provider.messages) != 0 {
		t.Fatalf("provider.messages = %#v, want no LLM call at all", provider.messages)
	}
}

// TestReflex_CaptureGroupSubstitution covers $1-style capture groups in
// reply templates.
func TestReflex_CaptureGroupSubstitution(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^lembra de (.+)$`, Action: config.ReflexActionReply, Template: "Anotado: $1"},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	response, err := al.processMessage(context.Background(), inboundReflexMessage("lembra de comprar pao"))
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if response != "Anotado: comprar pao" {
		t.Fatalf("response = %q, want capture group substituted", response)
	}
}

// TestReflex_CommandActionRunsSlashCommand covers AC-013-2: a matched
// "command" reflex runs the existing slash command executor.
func TestReflex_CommandActionRunsSlashCommand(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^status$`, Action: config.ReflexActionCommand, Template: "/context"},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	response, err := al.processMessage(context.Background(), inboundReflexMessage("status"))
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if !strings.Contains(response, "Context usage") && !strings.Contains(response, "No active session context") {
		t.Fatalf("response = %q, want /context output", response)
	}
	if len(provider.messages) != 0 {
		t.Fatalf("provider.messages = %#v, want no LLM call", provider.messages)
	}
}

// TestReflex_ExecActionInvokesExecTool covers AC-013-3: a matched "exec"
// reflex calls the exec tool through Tools.ExecuteWithContext (the same
// gated path a normal tool call uses) and returns its result.
func TestReflex_ExecActionInvokesExecTool(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^uptime$`, Action: config.ReflexActionExec, Template: "uptime"},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	execTool := &fakeExecTool{}
	al.RegisterTool(execTool)

	response, err := al.processMessage(context.Background(), inboundReflexMessage("uptime"))
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if response != "ran: uptime" {
		t.Fatalf("response = %q, want exec tool result", response)
	}
	if execTool.lastArgs["action"] != "run" || execTool.lastArgs["command"] != "uptime" {
		t.Fatalf("exec tool args = %#v, want action=run command=uptime", execTool.lastArgs)
	}
}

// TestReflex_PersistDefaults covers AC-013-4: reply/command persist by
// default, exec doesn't. Exercised via tryReflex directly (rather than
// processMessage's full routing) so the test controls the session key
// precisely instead of guessing what routing/session allocation would
// produce.
func TestReflex_PersistDefaults(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^oi$`, Action: config.ReflexActionReply, Template: "Oi!"},
					{Match: `(?i)^uptime$`, Action: config.ReflexActionExec, Template: "uptime"},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&fakeExecTool{})
	agent := al.GetRegistry().GetDefaultAgent()

	replyKey := "reflex-persist-reply"
	replyOpts := &processOptions{Dispatch: DispatchRequest{SessionKey: replyKey}}
	if _, matched := al.tryReflex(context.Background(), agent, inboundReflexMessage("oi"), replyOpts); !matched {
		t.Fatal("tryReflex(reply) matched = false, want true")
	}
	if history := agent.Sessions.GetHistory(replyKey); len(history) == 0 {
		t.Fatal("reply reflex did not persist to session history, want persisted by default")
	}

	execKey := "reflex-persist-exec"
	execOpts := &processOptions{Dispatch: DispatchRequest{SessionKey: execKey}}
	if _, matched := al.tryReflex(context.Background(), agent, inboundReflexMessage("uptime"), execOpts); !matched {
		t.Fatal("tryReflex(exec) matched = false, want true")
	}
	if history := agent.Sessions.GetHistory(execKey); len(history) != 0 {
		t.Fatalf("exec reflex persisted %d messages, want 0 (default persist=false)", len(history))
	}
}

// TestReflex_PersistExplicitOverride covers the override half of AC-013-4:
// an exec reflex with Persist=true explicitly must still persist.
func TestReflex_PersistExplicitOverride(t *testing.T) {
	persistTrue := true
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^uptime$`, Action: config.ReflexActionExec, Template: "uptime", Persist: &persistTrue},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	al.RegisterTool(&fakeExecTool{})
	agent := al.GetRegistry().GetDefaultAgent()

	key := "reflex-persist-override"
	opts := &processOptions{Dispatch: DispatchRequest{SessionKey: key}}
	if _, matched := al.tryReflex(context.Background(), agent, inboundReflexMessage("uptime"), opts); !matched {
		t.Fatal("tryReflex(exec, persist override) matched = false, want true")
	}
	if history := agent.Sessions.GetHistory(key); len(history) == 0 {
		t.Fatal("exec reflex with Persist=true did not persist, want persisted")
	}
}

// TestReflex_NoReflexesConfiguredFallsThroughToLLM covers AC-013-5: with no
// reflexes configured (the default), a message that would have matched
// nothing falls straight through to the LLM — the existing pipeline is
// untouched.
func TestReflex_NoReflexesConfiguredFallsThroughToLLM(t *testing.T) {
	cfg := &config.Config{}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	response, err := al.processMessage(context.Background(), inboundReflexMessage("oi"))
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if response != "profile response" {
		t.Fatalf("response = %q, want the LLM's canned response (no reflex intercepted it)", response)
	}
	if len(provider.messages) == 0 {
		t.Fatal("provider.messages empty, want the LLM to have been called")
	}
}

// TestReflex_NonMatchingMessageFallsThroughToLLM covers the same
// compatibility guarantee for a config that DOES have reflexes, but none
// of them match this particular message.
func TestReflex_NonMatchingMessageFallsThroughToLLM(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^oi$`, Action: config.ReflexActionReply, Template: "Oi!"},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	response, err := al.processMessage(context.Background(), inboundReflexMessage("tell me a long story"))
	if err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if response != "profile response" {
		t.Fatalf("response = %q, want the LLM's canned response", response)
	}
}

// TestReflex_CronOriginNeverTriggersReflexes documents that reflexes only
// run for origin "user" (ADR-014 point 6 / FR-013): a cron-dispatched
// message (SenderID="cron") must reach the LLM even if it matches a
// configured reflex.
func TestReflex_CronOriginNeverTriggersReflexes(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Reflexes: []config.ReflexConfig{
					{Match: `(?i)^oi$`, Action: config.ReflexActionReply, Template: "Oi!"},
				},
			},
		},
	}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)

	response, err := al.ProcessDirectWithChannel(context.Background(), "oi", "agent:cron-test", "cli", "direct")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	if response != "profile response" {
		t.Fatalf("response = %q, want LLM response (cron origin bypasses reflexes)", response)
	}
	if len(provider.messages) == 0 {
		t.Fatal("provider.messages empty, want the LLM to have been called for a cron-origin message")
	}
}
