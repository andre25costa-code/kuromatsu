package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// Trilho G B.1/B.3: per-agent heartbeat dispatch and the /agent pin.

// isolateKuromatsuHome points KUROMATSU_HOME at a fresh temp dir for the
// duration of the test -- NewAgentLoop unconditionally calls
// loadAgentPins() (agent_pin.go) at construction, and any test that pins/
// unpins writes agent-pins.json, so every test in this file must not read
// or write the real user's $KUROMATSU_HOME.
func isolateKuromatsuHome(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvHome, filepath.Join(t.TempDir(), "kuromatsu-home"))
}

func TestProcessHeartbeatForAgent_UnknownAgentIDReturnsError(t *testing.T) {
	isolateKuromatsuHome(t)
	cfg := testCfg([]config.AgentConfig{
		{ID: "sensores", Default: true},
	})
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockRegistryProvider{})
	defer al.Close()

	_, err := al.ProcessHeartbeatForAgent(context.Background(), "does-not-exist", "check", "", "")
	if err == nil {
		t.Fatal("ProcessHeartbeatForAgent() error = nil, want an error for an unregistered agent")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("error = %q, want it to name the unknown agent id", err.Error())
	}
}

func TestProcessHeartbeatForAgent_RunsOnTargetAgentWorkspace(t *testing.T) {
	isolateKuromatsuHome(t)
	sensoresWorkspace := setupWorkspace(t, map[string]string{
		"AGENT.md": "# Agent\nYou are Sensores, the irrigation and sensor agent.",
	})
	defer cleanupWorkspace(t, sensoresWorkspace)

	estudosWorkspace := setupWorkspace(t, map[string]string{
		"AGENT.md": "# Agent\nYou are Estudos, the study assistant.",
	})
	defer cleanupWorkspace(t, estudosWorkspace)

	cfg := testCfg([]config.AgentConfig{
		{ID: "sensores", Workspace: sensoresWorkspace},
		{ID: "estudos", Default: true, Workspace: estudosWorkspace},
	})

	provider := &turnProfileCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	defer al.Close()

	if _, err := al.ProcessHeartbeatForAgent(context.Background(), "sensores", "check tasks", "", ""); err != nil {
		t.Fatalf("ProcessHeartbeatForAgent(sensores) error = %v", err)
	}
	if len(provider.messages) == 0 {
		t.Fatal("expected at least one captured message")
	}
	systemPrompt := provider.messages[0].Content
	if !strings.Contains(systemPrompt, "Sensores") {
		t.Fatalf("system prompt for agent %q missing its own AGENT.md content:\n%s", "sensores", systemPrompt)
	}
	if strings.Contains(systemPrompt, "Estudos") {
		t.Fatalf("system prompt for agent %q leaked the other agent's AGENT.md content:\n%s", "sensores", systemPrompt)
	}

	if _, err := al.ProcessHeartbeatForAgent(context.Background(), "estudos", "check tasks", "", ""); err != nil {
		t.Fatalf("ProcessHeartbeatForAgent(estudos) error = %v", err)
	}
	systemPrompt = provider.messages[0].Content
	if !strings.Contains(systemPrompt, "Estudos") {
		t.Fatalf("system prompt for agent %q missing its own AGENT.md content:\n%s", "estudos", systemPrompt)
	}
	if strings.Contains(systemPrompt, "Sensores") {
		t.Fatalf("system prompt for agent %q leaked the other agent's AGENT.md content:\n%s", "estudos", systemPrompt)
	}
}

func TestResolveMessageRoute_PinTakesPrecedenceOverDispatchRule(t *testing.T) {
	isolateKuromatsuHome(t)
	cfg := testCfg([]config.AgentConfig{
		{ID: "sensores"},
		{ID: "estudos", Default: true},
	})
	cfg.Agents.Dispatch = &config.DispatchConfig{
		Rules: []config.DispatchRule{
			{Name: "telegram-to-estudos", Agent: "estudos", When: config.DispatchSelector{Channel: "telegram"}},
		},
	}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockRegistryProvider{})
	defer al.Close()

	msg := bus.InboundMessage{
		Context: bus.InboundContext{Channel: "telegram", ChatID: "chat1", SenderID: "user1"},
		Content: "hello",
	}

	route, agent, err := al.resolveMessageRoute(msg)
	if err != nil {
		t.Fatalf("resolveMessageRoute() error = %v", err)
	}
	if route.AgentID != "estudos" || agent.ID != "estudos" {
		t.Fatalf("baseline route = %+v (agent %q), want agent %q via dispatch rule", route, agent.ID, "estudos")
	}

	al.pinAgentID(chatPinKey(msg.Context), "sensores")

	route, agent, err = al.resolveMessageRoute(msg)
	if err != nil {
		t.Fatalf("resolveMessageRoute() after pin error = %v", err)
	}
	if route.AgentID != "sensores" || agent.ID != "sensores" {
		t.Fatalf("pinned route = %+v (agent %q), want agent %q (pin must win over the dispatch rule)", route, agent.ID, "sensores")
	}
	if route.MatchedBy != "agent.pin" {
		t.Fatalf("route.MatchedBy = %q, want %q", route.MatchedBy, "agent.pin")
	}

	al.unpinAgentID(chatPinKey(msg.Context))
	route, agent, err = al.resolveMessageRoute(msg)
	if err != nil {
		t.Fatalf("resolveMessageRoute() after unpin error = %v", err)
	}
	if route.AgentID != "estudos" || agent.ID != "estudos" {
		t.Fatalf("post-unpin route = %+v (agent %q), want back to %q via dispatch rule", route, agent.ID, "estudos")
	}
}

func TestApplyExplicitAgentCommand_PinAndAuto(t *testing.T) {
	isolateKuromatsuHome(t)
	cfg := testCfg([]config.AgentConfig{
		{ID: "sensores"},
		{ID: "estudos", Default: true},
	})
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockRegistryProvider{})
	defer al.Close()

	opts := &processOptions{
		Dispatch: DispatchRequest{
			InboundContext: &bus.InboundContext{Channel: "test", ChatID: "chat1"},
		},
	}

	matched, handled, reply := al.applyExplicitAgentCommand("/agent estudos", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want both true", matched, handled)
	}
	if !strings.Contains(reply, "estudos") {
		t.Fatalf("reply = %q, want it to mention the pinned agent", reply)
	}

	key := chatPinKey(*opts.Dispatch.InboundContext)
	if pinned, ok := al.pinnedAgentID(key); !ok || pinned != "estudos" {
		t.Fatalf("pinnedAgentID() = (%q, %v), want (%q, true)", pinned, ok, "estudos")
	}

	// No-args form reports the current pin.
	matched, handled, reply = al.applyExplicitAgentCommand("/agent", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want both true", matched, handled)
	}
	if !strings.Contains(reply, "estudos") {
		t.Fatalf("status reply = %q, want it to mention the current pin", reply)
	}

	matched, handled, reply = al.applyExplicitAgentCommand("/agent auto", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want both true", matched, handled)
	}
	if !strings.Contains(strings.ToLower(reply), "cleared") {
		t.Fatalf("reply = %q, want it to confirm the pin was cleared", reply)
	}
	if _, ok := al.pinnedAgentID(key); ok {
		t.Fatal("pin still present after /agent auto")
	}
}

// TestProcessDirectWithChannel_RoutesViaDispatchRules is Trilho G B.2's
// "already works today" claim, proven for real: ProcessDirectWithChannel
// (what CronTool.ExecuteJob calls whenever payload.AgentID is unset) goes
// through processMessage -> resolveMessageRoute -> the same
// agents.dispatch.rules a Telegram/WhatsApp message would -- a cron job
// whose Channel/To matches a rule already lands on that rule's agent, with
// zero code changes beyond what B.1/B.3 already added for other reasons.
func TestProcessDirectWithChannel_RoutesViaDispatchRules(t *testing.T) {
	isolateKuromatsuHome(t)
	sensoresWorkspace := setupWorkspace(t, map[string]string{
		"AGENT.md": "# Agent\nYou are Sensores.",
	})
	defer cleanupWorkspace(t, sensoresWorkspace)

	cfg := testCfg([]config.AgentConfig{
		{ID: "sensores", Workspace: sensoresWorkspace},
		{ID: "estudos", Default: true},
	})
	cfg.Agents.Dispatch = &config.DispatchConfig{
		Rules: []config.DispatchRule{
			{Name: "whatsapp-to-sensores", Agent: "sensores", When: config.DispatchSelector{Channel: "whatsapp"}},
		},
	}

	provider := &turnProfileCaptureProvider{}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	defer al.Close()

	resp, err := al.ProcessDirectWithChannel(context.Background(), "check irrigation", "", "whatsapp", "chat-1")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	_ = resp
	if len(provider.messages) == 0 {
		t.Fatal("expected at least one captured message")
	}
	if !strings.Contains(provider.messages[0].Content, "Sensores") {
		t.Fatalf("system prompt = %q, want it to come from the sensores agent (routed via dispatch rule)", provider.messages[0].Content)
	}
}

func TestApplyExplicitAgentCommand_UnknownAgentDoesNotPin(t *testing.T) {
	isolateKuromatsuHome(t)
	cfg := testCfg([]config.AgentConfig{
		{ID: "sensores"},
		{ID: "estudos", Default: true},
	})
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockRegistryProvider{})
	defer al.Close()

	opts := &processOptions{
		Dispatch: DispatchRequest{
			InboundContext: &bus.InboundContext{Channel: "test", ChatID: "chat1"},
		},
	}
	key := chatPinKey(*opts.Dispatch.InboundContext)

	al.pinAgentID(key, "estudos")

	matched, handled, reply := al.applyExplicitAgentCommand("/agent nao-existe", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want both true", matched, handled)
	}
	if !strings.Contains(strings.ToLower(reply), "unknown") {
		t.Fatalf("reply = %q, want it to report the agent as unknown", reply)
	}
	if pinned, ok := al.pinnedAgentID(key); !ok || pinned != "estudos" {
		t.Fatalf("pinnedAgentID() = (%q, %v), want unchanged (%q, true)", pinned, ok, "estudos")
	}
}
