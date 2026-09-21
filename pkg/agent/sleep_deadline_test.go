package agent

import (
	"context"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/sleep"
)

type deadlineSessionSource struct{}

func (deadlineSessionSource) RecentDigests(context.Context, string, time.Time, bool) ([]sleep.SessionDigest, error) {
	return []sleep.SessionDigest{{SessionID: "s", Summary: "fact"}}, nil
}

func TestSleepWindowCancelsInFlightConsolidation(t *testing.T) {
	called := make(chan struct{})
	runtime := sleep.NewRuntime(
		sleep.Config{Enabled: true},
		deadlineSessionSource{},
		func(ctx context.Context, _, _ string) (sleep.ChatResult, error) {
			close(called)
			<-ctx.Done()
			return sleep.ChatResult{}, ctx.Err()
		},
	)
	b := &sleepBridge{runtime: runtime, bgCtx: context.Background(), workspaces: []string{t.TempDir()}}
	done := make(chan struct{})
	go func() { b.runWindowUntil(time.Now().Add(50 * time.Millisecond)); close(done) }()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("window did not cancel the provider")
	}
}
