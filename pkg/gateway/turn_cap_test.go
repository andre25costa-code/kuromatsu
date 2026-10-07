package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// AC-028-5: heartbeat and cron turns get a deadline; 0 disables it.
func TestNonUserTurnContext_AppliesDeadline(t *testing.T) {
	ctx, cancel := nonUserTurnContext(context.Background(), 60)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("no deadline set for a 60-minute cap")
	}
	if left := time.Until(deadline); left < 59*time.Minute || left > 60*time.Minute {
		t.Fatalf("deadline in %v, want ~60m", left)
	}
}

func TestNonUserTurnContext_ZeroDisablesCap(t *testing.T) {
	ctx, cancel := nonUserTurnContext(context.Background(), 0)
	defer cancel()

	if _, ok := ctx.Deadline(); ok {
		t.Fatal("deadline set with the cap disabled (0)")
	}
}

func TestDefaultConfig_NonUserTurnCapIsOneHeartbeatInterval(t *testing.T) {
	if got := config.DefaultConfig().Agents.Defaults.NonUserTurnMaxMinutes; got != 60 {
		t.Fatalf("default non_user_turn_max_minutes = %d, want 60", got)
	}
}
