package agent

import (
	"context"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/routing"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
	"github.com/andre25costa-code/kuromatsu/pkg/session"
)

type alwaysSucceedsProvider struct{}

func (alwaysSucceedsProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: "ok"}, nil
}

func (alwaysSucceedsProvider) GetDefaultModel() string { return "always-succeeds-mock" }

// TestPipelineLLM_ErrBusy_UsesOwnWaitBudget_NotGenericBackoff is A.3's proof
// that a runstate.ErrBusy suspension waits on the real resume signal
// (runstate.Engine.Subscribe) instead of the generic
// max_llm_retries/llm_retry_backoff_secs fixed backoff. The runstate engine
// here is real (not a fake): activeRequestsInc's own gating produces the
// exact errRuntimeSuspendedOverloaded a live memguard suspension would.
//
// Timing is the proof: the default generic backoff is 2s (retry 0's
// backoff = (0+1)*2s). Resume() fires at ~300ms, well under that. If the
// ErrBusy branch is missing or short-circuited, the call would fall through
// to the generic path and take >=2s (sleep the fixed backoff, then retry
// regardless of whether the engine is actually resumed yet). Observing
// ~300ms total instead proves the dedicated wait path ran.
func TestPipelineLLM_ErrBusy_UsesOwnWaitBudget_NotGenericBackoff(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:              tmpDir,
				ModelName:              "test-model",
				MaxTokens:              4096,
				MaxToolIterations:      10,
				MaxLLMRetries:          2,
				LLMRetryBackoffSecs:    2,
				RunstateResumeWaitSecs: 5,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	al := NewAgentLoop(cfg, msgBus, alwaysSucceedsProvider{})

	rs := runstate.New()
	rs.Suspend()
	al.runstate = rs

	go func() {
		time.Sleep(300 * time.Millisecond)
		rs.Resume()
	}()

	sessionKey := session.BuildMainSessionKey(routing.DefaultAgentID)
	start := time.Now()
	resp, err := al.ProcessDirectWithChannel(context.Background(), "hi", sessionKey, "test", "chat1")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ProcessDirectWithChannel() error = %v", err)
	}
	if resp != "ok" {
		t.Fatalf("response = %q, want %q", resp, "ok")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("elapsed = %v, want well under the 2s generic backoff -- the ErrBusy branch should have resolved via Resume() at ~300ms, not the generic path", elapsed)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("elapsed = %v, want close to the ~300ms Resume() delay (too fast suggests it didn't actually wait on the suspension)", elapsed)
	}
}
