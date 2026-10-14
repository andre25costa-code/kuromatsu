package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
)

// ctxBlockingProvider blocks every Chat call until ctx ends.
type ctxBlockingProvider struct{}

func (ctxBlockingProvider) Chat(
	ctx context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]any,
) (*providers.LLMResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (ctxBlockingProvider) GetDefaultModel() string { return "ctx-blocking-model" }

// slowAnswerProvider answers after delay unless ctx ends first.
type slowAnswerProvider struct{ delay time.Duration }

func (p slowAnswerProvider) Chat(
	ctx context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]any,
) (*providers.LLMResponse, error) {
	select {
	case <-time.After(p.delay):
		return &providers.LLMResponse{Content: "slow answer"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (slowAnswerProvider) GetDefaultModel() string { return "slow-answer-model" }

// newUserTurnTimeoutLoop builds a loop whose user-turn cap is 30 minutes in
// config but 50ms in effect, so the error quotes the configured value.
func newUserTurnTimeoutLoop(t *testing.T, msgBus *bus.MessageBus, provider providers.LLMProvider) *AgentLoop {
	t.Helper()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:          t.TempDir(),
				ModelName:          "test-model",
				MaxTokens:          4096,
				MaxToolIterations:  5,
				UserTurnMaxMinutes: 30,
			},
		},
	}
	al := NewAgentLoop(cfg, msgBus, provider)
	t.Cleanup(al.Close)
	if al.userTurnTimeout != 30*time.Minute {
		t.Fatalf("userTurnTimeout = %v, want 30m from config", al.userTurnTimeout)
	}
	al.userTurnTimeout = 50 * time.Millisecond
	return al
}

func assertUserTurnTimeoutMessage(t *testing.T, msg string) {
	t.Helper()
	if got := strings.Count(msg, "user_turn_max_minutes"); got != 1 {
		t.Fatalf("message mentions user_turn_max_minutes %d times, want exactly 1: %q", got, msg)
	}
	if !strings.Contains(msg, "turn exceeded agents.defaults.user_turn_max_minutes (30 min)") {
		t.Fatalf("message %q does not name the setting and its value", msg)
	}
}

func TestProcessDirect_UserTurnTimeout(t *testing.T) {
	al := newUserTurnTimeoutLoop(t, bus.NewMessageBus(), ctxBlockingProvider{})

	start := time.Now()
	_, err := al.ProcessDirect(context.Background(), "hello", "user-turn-timeout")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ProcessDirect returned no error, want the user-turn deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v does not match context.DeadlineExceeded", err)
	}
	assertUserTurnTimeoutMessage(t, err.Error())
	if elapsed > 2*time.Second {
		t.Fatalf("ProcessDirect took %v, want < 2s", elapsed)
	}
	want := "Error processing message: turn exceeded agents.defaults.user_turn_max_minutes (30 min)"
	if got := formatProcessingError(err); !strings.HasPrefix(got, want) {
		t.Fatalf("user-facing error = %q, want prefix %q", got, want)
	}
}

func TestRun_UserTurnTimeoutPublishesError(t *testing.T) {
	msgBus := bus.NewMessageBus()
	defer msgBus.Close()
	al := newUserTurnTimeoutLoop(t, msgBus, ctxBlockingProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = al.Run(ctx)
	}()
	defer func() {
		cancel()
		<-runDone
	}()

	inbound := bus.InboundMessage{
		Context: bus.InboundContext{
			Channel:  "telegram",
			ChatID:   "chat-timeout",
			ChatType: "direct",
			SenderID: "user-timeout",
		},
		Channel:  "telegram",
		ChatID:   "chat-timeout",
		SenderID: "user-timeout",
		Content:  "hello",
	}
	if err := msgBus.PublishInbound(context.Background(), inbound); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case out := <-msgBus.OutboundChan():
			if !strings.Contains(out.Content, "user_turn_max_minutes") {
				continue
			}
			if !strings.HasPrefix(out.Content, "Error processing message: ") {
				t.Fatalf("outbound %q is not the processing-error message", out.Content)
			}
			assertUserTurnTimeoutMessage(t, out.Content)
			return
		case <-deadline:
			t.Fatal("no outbound message about user_turn_max_minutes within 5s")
		}
	}
}

func TestProcessCron_NotSubjectToUserTurnTimeout(t *testing.T) {
	al := newUserTurnTimeoutLoop(t, bus.NewMessageBus(), slowAnswerProvider{delay: 200 * time.Millisecond})

	response, err := al.ProcessCronWithChannel(
		context.Background(), "scheduled job", "cron-no-user-cap", "test", "chat1",
	)
	if err != nil {
		t.Fatalf("ProcessCronWithChannel: %v (the user-turn deadline must not apply to cron)", err)
	}
	if response != "slow answer" {
		t.Fatalf("response = %q, want %q", response, "slow answer")
	}
}
