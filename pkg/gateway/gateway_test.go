package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/agent"
	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	runtimeevents "github.com/andre25costa-code/kuromatsu/pkg/events"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
)

func TestRun_StartupFailuresReturnErrorAndEmitStructuredLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		prepare    func(t *testing.T, dir string) string
		wantErr    string
		wantLogSub string
	}{
		{
			name: "invalid config returns load error",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				cfgPath := filepath.Join(dir, "invalid-config.json")
				if err := os.WriteFile(cfgPath, []byte("{invalid-json"), 0o644); err != nil {
					t.Fatalf("WriteFile(invalid config) error = %v", err)
				}
				return cfgPath
			},
			wantErr:    "error loading config:",
			wantLogSub: "error loading config:",
		},
		{
			name: "invalid config returns pre-check error",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				cfg := config.DefaultConfig()
				cfg.Gateway.Port = 0
				cfgPath := filepath.Join(dir, "config.json")
				if err := config.SaveConfig(cfgPath, cfg); err != nil {
					t.Fatalf("SaveConfig() error = %v", err)
				}
				return cfgPath
			},
			wantErr:    "config pre-check failed: invalid gateway port: 0",
			wantLogSub: "config pre-check failed: invalid gateway port: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			homeDir := t.TempDir()
			configPath := tt.prepare(t, homeDir)

			cmd := exec.Command(os.Args[0], "-test.run=TestGatewayRunStartupFailureHelper")
			cmd.Env = append(os.Environ(),
				"GO_WANT_GATEWAY_RUN_HELPER=1",
				"PICO_TEST_HOME="+homeDir,
				"PICO_TEST_CONFIG="+configPath,
			)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("helper exited unexpectedly: %v\noutput:\n%s", err, string(output))
			}

			out := string(output)
			if !strings.Contains(out, tt.wantErr) {
				t.Fatalf("helper output missing expected error substring %q:\n%s", tt.wantErr, out)
			}

			logData, readErr := os.ReadFile(filepath.Join(homeDir, logPath, logFile))
			if readErr != nil {
				t.Fatalf("ReadFile(gateway.log) error = %v", readErr)
			}
			logText := string(logData)
			if !strings.Contains(logText, "Gateway startup failed") {
				t.Fatalf("gateway.log missing structured startup failure log:\n%s", logText)
			}
			if !strings.Contains(logText, tt.wantLogSub) {
				t.Fatalf("gateway.log missing expected failure detail %q:\n%s", tt.wantLogSub, logText)
			}
		})
	}
}

func TestGatewayRunStartupFailureHelper(t *testing.T) {
	if os.Getenv("GO_WANT_GATEWAY_RUN_HELPER") != "1" {
		return
	}

	homeDir := os.Getenv("PICO_TEST_HOME")
	configPath := os.Getenv("PICO_TEST_CONFIG")

	err := Run(false, homeDir, configPath, false)
	if err == nil {
		fmt.Fprintln(os.Stdout, "expected startup error, got nil")
		os.Exit(2)
	}

	fmt.Fprintln(os.Stdout, err.Error())
	os.Exit(0)
}

func TestCollectGatewayStartupStatusHandlesMalformedInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		startupInfo         map[string]any
		wantToolsCount      int
		wantSkillsAvailable int
		wantSkillsTotal     int
		wantLogFields       map[string]any
	}{
		{
			name:          "missing info",
			startupInfo:   map[string]any{},
			wantLogFields: map[string]any{},
		},
		{
			name: "wrong map shapes",
			startupInfo: map[string]any{
				"tools":  "unexpected",
				"skills": []any{"unexpected"},
			},
			wantLogFields: map[string]any{},
		},
		{
			name: "valid startup info",
			startupInfo: map[string]any{
				"tools": map[string]any{
					"count": 3,
				},
				"skills": map[string]any{
					"available": 2,
					"total":     5,
				},
			},
			wantToolsCount:      3,
			wantSkillsAvailable: 2,
			wantSkillsTotal:     5,
			wantLogFields: map[string]any{
				"tools_count":      3,
				"skills_available": 2,
				"skills_total":     5,
			},
		},
		{
			name: "json number startup info",
			startupInfo: map[string]any{
				"tools": map[string]any{
					"count": float64(4),
				},
				"skills": map[string]any{
					"available": float64(1),
					"total":     float64(6),
				},
			},
			wantToolsCount:      4,
			wantSkillsAvailable: 1,
			wantSkillsTotal:     6,
			wantLogFields: map[string]any{
				"tools_count":      4,
				"skills_available": 1,
				"skills_total":     6,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := collectGatewayStartupStatus(tt.startupInfo)
			if got.toolsCount != tt.wantToolsCount {
				t.Fatalf("toolsCount = %d, want %d", got.toolsCount, tt.wantToolsCount)
			}
			if got.skillsAvailable != tt.wantSkillsAvailable {
				t.Fatalf("skillsAvailable = %d, want %d", got.skillsAvailable, tt.wantSkillsAvailable)
			}
			if got.skillsTotal != tt.wantSkillsTotal {
				t.Fatalf("skillsTotal = %d, want %d", got.skillsTotal, tt.wantSkillsTotal)
			}
			if !reflect.DeepEqual(got.logFields, tt.wantLogFields) {
				t.Fatalf("logFields = %#v, want %#v", got.logFields, tt.wantLogFields)
			}
		})
	}
}

func TestPublishGatewayEvent(t *testing.T) {
	eventBus := runtimeevents.NewBus()
	t.Cleanup(func() {
		if err := eventBus.Close(); err != nil {
			t.Fatalf("Close runtime event bus: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sub, eventsCh, err := eventBus.Channel().OfKind(runtimeevents.KindGatewayStart).SubscribeChan(
		ctx,
		runtimeevents.SubscribeOptions{Name: "gateway-test", Buffer: 4},
	)
	if err != nil {
		t.Fatalf("SubscribeChan() error = %v", err)
	}
	t.Cleanup(func() {
		if err := sub.Close(); err != nil {
			t.Fatalf("Close subscription: %v", err)
		}
	})

	al := agent.NewAgentLoop(
		config.DefaultConfig(),
		bus.NewMessageBus(),
		&startupBlockedProvider{reason: "not used"},
		agent.WithRuntimeEvents(eventBus),
	)
	t.Cleanup(al.Close)

	startedAt := time.Now().Add(-1500 * time.Millisecond)
	publishGatewayEvent(al, runtimeevents.KindGatewayStart, startedAt, nil)

	evt := receiveGatewayRuntimeEvent(t, eventsCh)
	if evt.Kind != runtimeevents.KindGatewayStart ||
		evt.Source.Component != "gateway" ||
		evt.Severity != runtimeevents.SeverityInfo {
		t.Fatalf("gateway event = %+v", evt)
	}
	payload, ok := evt.Payload.(gatewayEventPayload)
	if !ok {
		t.Fatalf("payload type = %T, want gatewayEventPayload", evt.Payload)
	}
	if payload.DurationMS <= 0 {
		t.Fatalf("DurationMS = %d, want positive", payload.DurationMS)
	}
	if evt.Attrs["duration_ms"] == nil {
		t.Fatalf("gateway event attrs missing duration_ms: %#v", evt.Attrs)
	}
}

type ctxProbeKey struct{}

// ctxCapturingChatProvider records the ctx it receives in Chat(), so a test
// can check which ctx object actually reached the LLM call.
type ctxCapturingChatProvider struct {
	called      bool
	capturedCtx context.Context
}

func (p *ctxCapturingChatProvider) Chat(
	ctx context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.called = true
	p.capturedCtx = ctx
	return &providers.LLMResponse{Content: "HEARTBEAT_OK"}, nil
}

func (p *ctxCapturingChatProvider) GetDefaultModel() string { return "" }

// TestCreateHeartbeatHandlerForAgent_PropagatesGatewayContext is a real
// production bug this deploy found: the heartbeat handler used to call
// agentLoop.ProcessHeartbeat(context.Background(), ...) instead of
// forwarding the ctx it (now) receives -- so a heartbeat-originated turn in
// flight during shutdown never saw cancel() at all, and Fix 2's
// abort_callback wiring never fired for it. Confirmed for real on
// demetrius: two SIGKILLs on heartbeat turns during this same deploy,
// before this fix. A value stashed on the outer ctx (rather than checking
// Err()) proves the exact ctx object reaches Chat(), regardless of whether
// some earlier step in ProcessHeartbeat might otherwise short-circuit on a
// canceled context before ever calling it.
func TestCreateHeartbeatHandlerForAgent_PropagatesGatewayContext(t *testing.T) {
	fake := &ctxCapturingChatProvider{}
	al := agent.NewAgentLoop(config.DefaultConfig(), bus.NewMessageBus(), fake)
	t.Cleanup(al.Close)

	ctx := context.WithValue(context.Background(), ctxProbeKey{}, "gateway-ctx-marker")

	handler := createHeartbeatHandlerForAgent(ctx, al, "main")
	handler("check heartbeat tasks", "telegram", "chat-1")

	if !fake.called {
		t.Fatal("provider.Chat was never called")
	}
	if got, _ := fake.capturedCtx.Value(ctxProbeKey{}).(string); got != "gateway-ctx-marker" {
		t.Fatalf("ctx observed by provider.Chat carries marker %q, want %q -- createHeartbeatHandlerForAgent must propagate the gateway's own ctx, not context.Background()", got, "gateway-ctx-marker")
	}
}

// ctxCapturingProvider is a StatefulProvider whose Close() (invoked from
// inside shutdownGateway, see gateway.go's "cp.Close()" call) snapshots
// ctx.Err() at the moment it runs -- this is what actually distinguishes
// "cancel() ran before the shutdown sequence's internals" from "cancel()
// ran before initiateShutdown returned", which is true either way since
// initiateShutdown's two statements are sequential regardless of ordering.
type ctxCapturingProvider struct {
	startupBlockedProvider
	ctx        context.Context
	closedWith error
	closed     bool
}

func (p *ctxCapturingProvider) Close() {
	p.closed = true
	p.closedWith = p.ctx.Err()
}

// TestInitiateShutdown_CancelsContextBeforeShutdownSequence is Fix 2 (the
// graceful-shutdown gap the S39 benchmark found: an in-flight decode used
// to block shutdown up to systemd's 90s TimeoutStopSec and get SIGKILLed).
// ctx must already be Done by the time shutdownGateway's internals run
// (specifically, by the time the provider's Close() fires), not only
// afterward via Run's own deferred cancel().
func TestInitiateShutdown_CancelsContextBeforeShutdownSequence(t *testing.T) {
	msgBus := bus.NewMessageBus()
	al := agent.NewAgentLoop(config.DefaultConfig(), msgBus, &startupBlockedProvider{reason: "not used"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if ctx.Err() != nil {
		t.Fatal("ctx already Done before initiateShutdown ran")
	}

	spy := &ctxCapturingProvider{startupBlockedProvider: startupBlockedProvider{reason: "not used"}, ctx: ctx}
	initiateShutdown(cancel, &services{}, al, spy, msgBus)

	if !spy.closed {
		t.Fatal("provider.Close() was never called -- shutdownGateway's fullShutdown path did not run")
	}
	if !errors.Is(spy.closedWith, context.Canceled) {
		t.Fatalf("ctx.Err() at provider.Close() time = %v, want context.Canceled (cancel() must run before shutdownGateway's internals, not just before initiateShutdown returns)", spy.closedWith)
	}
}

// TestGateway_CreatesOneHeartbeatServicePerEnabledAgent is Trilho G B.1:
// startHeartbeatServices creates exactly one HeartbeatService per
// registered agent that resolves EffectiveHeartbeat().Enabled, keyed by
// agent ID, and none for an agent whose override disables it.
func TestGateway_CreatesOneHeartbeatServicePerEnabledAgent(t *testing.T) {
	disabled := false
	cfg := config.DefaultConfig()
	cfg.Heartbeat = config.HeartbeatConfig{Enabled: true, Interval: 60}
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Agents.List = []config.AgentConfig{
		{ID: "sensores", Default: true, Workspace: t.TempDir()},
		{ID: "estudos", Workspace: t.TempDir(), Heartbeat: &config.AgentHeartbeatConfig{Enabled: &disabled}},
	}

	msgBus := bus.NewMessageBus()
	al := agent.NewAgentLoop(cfg, msgBus, &startupBlockedProvider{reason: "not used"})
	t.Cleanup(al.Close)

	runningServices := &services{}
	if err := startHeartbeatServices(context.Background(), cfg, al, msgBus, runningServices); err != nil {
		t.Fatalf("startHeartbeatServices() error = %v", err)
	}
	t.Cleanup(func() {
		for _, svc := range runningServices.HeartbeatServices {
			svc.Stop()
		}
	})

	if len(runningServices.HeartbeatServices) != 1 {
		t.Fatalf("len(HeartbeatServices) = %d, want 1 (sensores enabled, estudos overridden off): %v",
			len(runningServices.HeartbeatServices), runningServices.HeartbeatServices)
	}
	if _, ok := runningServices.HeartbeatServices["sensores"]; !ok {
		t.Fatalf("HeartbeatServices missing key %q, got %v", "sensores", runningServices.HeartbeatServices)
	}
	if _, ok := runningServices.HeartbeatServices["estudos"]; ok {
		t.Fatal("HeartbeatServices has an entry for estudos, which has heartbeat.enabled=false")
	}
}

func TestShutdownGatewayClosesMessageBus(t *testing.T) {
	msgBus := bus.NewMessageBus()
	al := agent.NewAgentLoop(
		config.DefaultConfig(),
		msgBus,
		&startupBlockedProvider{reason: "not used"},
	)
	msgBus.SetEventPublisher(al.RuntimeEventBus())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, eventsCh, err := al.RuntimeEventBus().Channel().OfKind(runtimeevents.KindBusCloseCompleted).SubscribeChan(
		ctx,
		runtimeevents.SubscribeOptions{Name: "bus-close-test", Buffer: 4},
	)
	if err != nil {
		t.Fatalf("SubscribeChan() error = %v", err)
	}
	defer func() {
		_ = sub.Close()
	}()

	shutdownGateway(&services{}, al, &startupBlockedProvider{reason: "not used"}, msgBus, true)

	evt := receiveGatewayRuntimeEvent(t, eventsCh)
	if evt.Kind != runtimeevents.KindBusCloseCompleted {
		t.Fatalf("shutdown event kind = %q, want %q", evt.Kind, runtimeevents.KindBusCloseCompleted)
	}
	if err := msgBus.PublishVoiceControl(context.Background(), bus.VoiceControl{}); !errors.Is(err, bus.ErrBusClosed) {
		t.Fatalf("PublishVoiceControl after shutdown error = %v, want %v", err, bus.ErrBusClosed)
	}
}

func receiveGatewayRuntimeEvent(t *testing.T, ch <-chan runtimeevents.Event) runtimeevents.Event {
	t.Helper()

	select {
	case evt := <-ch:
		return evt
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for gateway runtime event")
		return runtimeevents.Event{}
	}
}
