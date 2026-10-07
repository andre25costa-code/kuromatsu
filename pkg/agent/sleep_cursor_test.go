package agent

import (
	"context"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/session"
)

func TestSleepCursorAcknowledgesSnapshotsAndSurvivesTruncation(t *testing.T) {
	workspace := t.TempDir()
	store := session.NewSessionManager(workspace)
	for i := 0; i < 10; i++ {
		store.AddMessage("s", "user", "old")
	}
	registry := &AgentRegistry{agents: map[string]*AgentInstance{"a": {ID: "a", Workspace: workspace, Sessions: store}}}
	source := newAgentSessionSource(registry)
	ctx := context.Background()
	digests, err := source.RecentDigests(ctx, workspace, time.Time{}, false)
	if err != nil || len(digests) != 1 {
		t.Fatalf("collect: %v %v", digests, err)
	}
	// Merely collecting does not advance the cursor.
	again, _ := source.RecentDigests(ctx, workspace, time.Time{}, false)
	if len(again) != 1 {
		t.Fatal("uncommitted digest disappeared")
	}
	if err := source.Acknowledge(ctx, workspace, digests); err != nil {
		t.Fatal(err)
	}
	// A new adapter after restart honors the persisted commit.
	source = newAgentSessionSource(registry)
	again, _ = source.RecentDigests(ctx, workspace, time.Time{}, false)
	if len(again) != 0 {
		t.Fatal("committed digest repeated after restart")
	}
	store.TruncateHistory("s", 1)
	store.AddMessage("s", "user", "new fact")
	again, _ = source.RecentDigests(ctx, workspace, time.Time{}, false)
	if len(again) != 1 {
		t.Fatal("new message hidden by smaller history length")
	}
}
