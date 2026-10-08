package agent

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func newConfiguredHookLoop(t *testing.T, provider *llmHookTestProvider, hooks config.HooksConfig) *AgentLoop {
	t.Helper()

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         t.TempDir(),
				ModelName:         "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
		Hooks: hooks,
	}

	return NewAgentLoop(cfg, bus.NewMessageBus(), provider)
}

func TestAgentLoop_ProcessDirectWithChannel_AutoMountsProcessHook(t *testing.T) {
	provider := &llmHookTestProvider{}
	eventLog := filepath.Join(t.TempDir(), "events.log")

	al := newConfiguredHookLoop(t, provider, config.HooksConfig{
		Enabled: true,
		Processes: map[string]config.ProcessHookConfig{
			"ipc-auto": {
				Enabled: true,
				Command: processHookHelperCommand(),
				Env: map[string]string{
					"KUROMATSU_HOOK_HELPER":    "1",
					"KUROMATSU_HOOK_MODE":      "rewrite",
					"KUROMATSU_HOOK_EVENT_LOG": eventLog,
				},
				Observe:   []string{"turn_end"},
				Intercept: []string{"before_llm", "after_llm"},
			},
		},
	})
	defer al.Close()

	resp, err := al.ProcessDirectWithChannel(context.Background(), "hello", "session-1", "cli", "direct")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel failed: %v", err)
	}
	if resp != "provider content|ipc" {
		t.Fatalf("expected process-hooked content, got %q", resp)
	}

	provider.mu.Lock()
	lastModel := provider.lastModel
	provider.mu.Unlock()
	if lastModel != "process-model" {
		t.Fatalf("expected process model, got %q", lastModel)
	}

	waitForFileContains(t, eventLog, "agent.turn.end")
}

func TestProcessHookObserveKindsFromConfigAcceptsRuntimeNames(t *testing.T) {
	kinds, enabled, err := processHookObserveKindsFromConfig([]string{
		"tool_exec_start",
		"agent.tool.exec_end",
		"gateway.ready",
		"mcp.server.failed",
	})
	if err != nil {
		t.Fatalf("processHookObserveKindsFromConfig failed: %v", err)
	}
	if !enabled {
		t.Fatal("expected observe to be enabled")
	}

	want := []string{"agent.tool.exec_start", "agent.tool.exec_end", "gateway.ready", "mcp.server.failed"}
	if !slices.Equal(kinds, want) {
		t.Fatalf("observe kinds = %v, want %v", kinds, want)
	}
}

func TestAgentLoop_ProcessDirectWithChannel_InvalidConfiguredHookFails(t *testing.T) {
	provider := &llmHookTestProvider{}
	al := newConfiguredHookLoop(t, provider, config.HooksConfig{
		Enabled: true,
		Processes: map[string]config.ProcessHookConfig{
			"bad-hook": {
				Enabled:   true,
				Command:   processHookHelperCommand(),
				Intercept: []string{"not_supported"},
			},
		},
	})
	defer al.Close()

	_, err := al.ProcessDirectWithChannel(context.Background(), "hello", "session-1", "cli", "direct")
	if err == nil {
		t.Fatal("expected invalid configured hook error")
	}
}
