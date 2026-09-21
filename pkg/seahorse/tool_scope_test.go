package seahorse

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

func TestMemoryToolsEnforceConversationScope(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a, _ := s.GetOrCreateConversation(ctx, "agent:a:chat:one")
	b, _ := s.GetOrCreateConversation(ctx, "agent:b:chat:two")
	_, err := s.AddMessage(ctx, a.ConversationID, "user", "sharedword PUBLIC_A", 5)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.AddMessage(ctx, b.ConversationID, "user", "sharedword PRIVATE_B", 5)
	if err != nil {
		t.Fatal(err)
	}
	engine := &RetrievalEngine{store: s}
	grep := NewGrepTool(engine)
	expand := NewExpandTool(engine)
	owner := tools.WithToolSessionContext(ctx, "a", a.SessionKey, nil)
	for _, pattern := range []string{"sharedword", "%sharedword%"} {
		result := grep.Execute(owner, map[string]any{"pattern": pattern})
		if result.IsError || !strings.Contains(result.ForLLM, "PUBLIC_A") ||
			strings.Contains(result.ForLLM, "PRIVATE_B") {
			t.Fatalf("scope failed: %+v", result)
		}
	}
	if !grep.Execute(owner, map[string]any{"pattern": "sharedword", "all_conversations": true}).IsError {
		t.Fatal("model-controlled widening was accepted")
	}
	if !grep.Execute(ctx, map[string]any{"pattern": "sharedword"}).IsError {
		t.Fatal("missing identity was accepted")
	}
	if !expand.Execute(owner, map[string]any{"message_ids": []any{fmt.Sprint(secret.ID)}}).IsError {
		t.Fatal("foreign message expanded")
	}
}
