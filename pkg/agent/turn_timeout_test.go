package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	runtimeevents "github.com/andre25costa-code/kuromatsu/pkg/events"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
)

// blockingProvider stands in for a native generation that never finishes
// on its own: it returns only when the turn's context ends.
type blockingProvider struct{}

func (blockingProvider) Chat(
	ctx context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]any,
) (*providers.LLMResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingProvider) GetDefaultModel() string { return "blocking-model" }

// AC-028-5: a non-user turn cut by its deadline ends with status "timeout"
// (not "error"), so telemetry can tell a capped runaway turn apart from a
// genuine failure -- the 755-minute cron turn of 2026-09-27 is the case.
func TestRunTurn_DeadlineExceededEndsWithTimeoutStatus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-timeout-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{
		Workspace: tmpDir, ModelName: "test-model", MaxTokens: 4096, MaxToolIterations: 3,
	}}}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), blockingProvider{})
	events, closeEvents := subscribeRuntimeEventsForTest(t, al, 16, runtimeevents.KindAgentTurnEnd)
	defer closeEvents()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _ = al.runAgentLoop(ctx, al.registry.GetDefaultAgent(), processOptions{
		SessionKey:      "heartbeat",
		UserMessage:     "heartbeat check",
		DefaultResponse: defaultResponse,
		Origin:          OriginHeartbeat,
		NoHistory:       true,
	})

	select {
	case evt := <-events:
		payload, ok := evt.Payload.(TurnEndPayload)
		if !ok {
			t.Fatalf("payload = %T, want TurnEndPayload", evt.Payload)
		}
		if payload.Status != TurnEndStatusTimeout {
			t.Fatalf("turn end status = %q, want %q", payload.Status, TurnEndStatusTimeout)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no turn end event")
	}
}

func TestRuntimeSeverity_TimeoutTurnIsWarn(t *testing.T) {
	got := runtimeSeverityForAgentEvent(runtimeevents.KindAgentTurnEnd, TurnEndPayload{Status: TurnEndStatusTimeout})
	if got != runtimeevents.SeverityWarn {
		t.Fatalf("severity = %v, want %v", got, runtimeevents.SeverityWarn)
	}
}
