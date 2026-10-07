package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	runtimeevents "github.com/andre25costa-code/kuromatsu/pkg/events"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

func telemetryTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Telemetry: config.TelemetryConfig{
			Enabled: true,
			DBPath:  filepath.Join(t.TempDir(), "turns.db"),
		},
	}
}

func TestNewTelemetryBridge_DisabledReturnsNil(t *testing.T) {
	cfg := &config.Config{Telemetry: config.TelemetryConfig{Enabled: false}}
	b, err := newTelemetryBridge(cfg, nil)
	if err != nil {
		t.Fatalf("newTelemetryBridge(disabled): %v", err)
	}
	if b != nil {
		t.Fatal("newTelemetryBridge(disabled) != nil, want nil (AC-019-6)")
	}
}

func TestNewTelemetryBridge_NilConfigReturnsNil(t *testing.T) {
	b, err := newTelemetryBridge(nil, nil)
	if err != nil {
		t.Fatalf("newTelemetryBridge(nil cfg): %v", err)
	}
	if b != nil {
		t.Fatal("newTelemetryBridge(nil cfg) != nil")
	}
}

func TestNewTelemetryBridge_EnabledOpensStoreAndCloses(t *testing.T) {
	cfg := telemetryTestConfig(t)
	b, err := newTelemetryBridge(cfg, nil)
	if err != nil {
		t.Fatalf("newTelemetryBridge: %v", err)
	}
	if b == nil {
		t.Fatal("newTelemetryBridge(enabled) = nil, want a bridge")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close must be safe to call again (mirrors evolutionBridge/
	// sleepBridge's idempotent-Close convention).
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestTelemetryBridge_NilReceiverMethodsAreSafe(t *testing.T) {
	var b *telemetryBridge
	if err := b.Close(); err != nil {
		t.Fatalf("Close() on nil: %v, want nil", err)
	}
	b.recordReflex("session") // must not panic
	if _, err := b.queryStats(context.Background(), "", 0); err == nil {
		t.Fatal("queryStats() on nil bridge = nil error, want an error describing telemetry as disabled")
	}
	if got := b.modeBits(); got != 0 {
		t.Fatalf("modeBits() on nil bridge = %d, want 0", got)
	}
}

func TestTelemetryBridge_RuntimeBusTurnEndWritesRow(t *testing.T) {
	cfg := telemetryTestConfig(t)
	b, err := newTelemetryBridge(cfg, nil)
	if err != nil {
		t.Fatalf("newTelemetryBridge: %v", err)
	}
	defer b.Close()

	eventBus := runtimeevents.NewBus()
	defer eventBus.Close()
	if err := b.subscribeRuntimeEvents(eventBus.Channel()); err != nil {
		t.Fatalf("subscribeRuntimeEvents: %v", err)
	}

	result := eventBus.Publish(context.Background(), runtimeevents.Event{
		Kind:   runtimeevents.KindAgentTurnEnd,
		Source: runtimeevents.Source{Component: "agent", Name: "main"},
		Scope: runtimeevents.Scope{
			AgentID:    "main",
			TurnID:     "turn-telemetry-1",
			SessionKey: "session-telemetry-1",
		},
		Payload: TurnEndPayload{
			Status:           TurnEndStatusCompleted,
			Iterations:       2,
			Duration:         1500 * time.Millisecond,
			FocusWindow:      "files",
			FocusEscalations: 1,
			Origin:           OriginUser,
			UnknownToolCalls: 0,
			PromptTokens:     300,
			CachedTokens:     150,
			OutputTokens:     40,
			PrefillMs:        200,
			GenMs:            800,
			ToolExecutions:   []ToolExecutionRecord{{Name: "exec", Success: true}},
		},
	})
	if result.Delivered == 0 {
		t.Fatalf("runtime bus publish delivered = %d, want > 0", result.Delivered)
	}

	waitForStatsCount(t, b, "files", 1)
}

func TestTelemetryBridge_IgnoresOtherEventKinds(t *testing.T) {
	cfg := telemetryTestConfig(t)
	b, err := newTelemetryBridge(cfg, nil)
	if err != nil {
		t.Fatalf("newTelemetryBridge: %v", err)
	}
	defer b.Close()

	if err := b.OnRuntimeEvent(context.Background(), runtimeevents.Event{
		Kind:    runtimeevents.KindAgentTurnStart,
		Payload: TurnStartPayload{},
	}); err != nil {
		t.Fatalf("OnRuntimeEvent(unrelated kind): %v", err)
	}
	// Give the (never-enqueued) recorder a moment, then confirm nothing landed.
	time.Sleep(20 * time.Millisecond)
	out, err := b.queryStats(context.Background(), "", 1)
	if err != nil {
		t.Fatalf("queryStats: %v", err)
	}
	if !strings.Contains(out, "no turns recorded") {
		t.Fatalf("queryStats() = %q, want no rows recorded", out)
	}
}

func TestTelemetryBridge_RecordReflexWritesZeroTokenRow(t *testing.T) {
	cfg := telemetryTestConfig(t)
	b, err := newTelemetryBridge(cfg, nil)
	if err != nil {
		t.Fatalf("newTelemetryBridge: %v", err)
	}
	defer b.Close()

	b.recordReflex("session-reflex")
	waitForStatsCount(t, b, "", 1)

	out, err := b.queryStats(context.Background(), "", 1)
	if err != nil {
		t.Fatalf("queryStats: %v", err)
	}
	if !strings.Contains(out, "(none)") {
		t.Fatalf("queryStats() = %q, want the empty-window row labeled (none)", out)
	}
}

func TestTelemetryBridge_ModeBitsReflectsRunstateSnapshot(t *testing.T) {
	rs := runstate.New()
	cfg := telemetryTestConfig(t)
	b, err := newTelemetryBridge(cfg, rs)
	if err != nil {
		t.Fatalf("newTelemetryBridge: %v", err)
	}
	defer b.Close()

	if got := b.modeBits(); got != 0 {
		t.Fatalf("modeBits() = %d, want 0 for an idle engine", got)
	}
	release := rs.Enter(runstate.ToolExec)
	defer release()
	if got := b.modeBits(); got != uint32(runstate.ToolExec) {
		t.Fatalf("modeBits() = %d, want %d (ToolExec active)", got, uint32(runstate.ToolExec))
	}
}

func TestTelemetryBridge_QueryStats_WindowFilterAndHours(t *testing.T) {
	cfg := telemetryTestConfig(t)
	b, err := newTelemetryBridge(cfg, nil)
	if err != nil {
		t.Fatalf("newTelemetryBridge: %v", err)
	}
	defer b.Close()

	eventBus := runtimeevents.NewBus()
	defer eventBus.Close()
	if err := b.subscribeRuntimeEvents(eventBus.Channel()); err != nil {
		t.Fatalf("subscribeRuntimeEvents: %v", err)
	}
	publishTurnEnd(t, eventBus, "files", 100, 50)
	publishTurnEnd(t, eventBus, "chat", 20, 5)
	waitForStatsCount(t, b, "files", 1)
	waitForStatsCount(t, b, "chat", 1)

	out, err := b.queryStats(context.Background(), "files", 24)
	if err != nil {
		t.Fatalf("queryStats: %v", err)
	}
	if !strings.Contains(out, "files") {
		t.Fatalf("queryStats(window=files) = %q, want the files row", out)
	}
	if strings.Contains(out, "chat") {
		t.Fatalf("queryStats(window=files) = %q, want the chat row filtered out", out)
	}
}

func publishTurnEnd(t *testing.T, bus runtimeevents.Bus, window string, promptTokens, outputTokens int) {
	t.Helper()
	result := bus.Publish(context.Background(), runtimeevents.Event{
		Kind:   runtimeevents.KindAgentTurnEnd,
		Source: runtimeevents.Source{Component: "agent", Name: "main"},
		Scope:  runtimeevents.Scope{AgentID: "main", SessionKey: "s"},
		Payload: TurnEndPayload{
			Status:       TurnEndStatusCompleted,
			FocusWindow:  window,
			PromptTokens: promptTokens,
			OutputTokens: outputTokens,
		},
	})
	if result.Delivered == 0 {
		t.Fatalf("publish for window %q delivered = %d, want > 0", window, result.Delivered)
	}
}

// waitForStatsCount polls queryStats until window's aggregate count
// reaches want (the Recorder writes asynchronously) or fails the test
// after a timeout.
func waitForStatsCount(t *testing.T, b *telemetryBridge, window string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	label := window
	if label == "" {
		label = "(none)"
	}
	for time.Now().Before(deadline) {
		out, err := b.queryStats(context.Background(), window, 24)
		if err != nil {
			t.Fatalf("queryStats: %v", err)
		}
		if strings.Contains(out, label) && !strings.Contains(out, "no turns recorded") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stats for window %q never reached count %d within the deadline", window, want)
}

