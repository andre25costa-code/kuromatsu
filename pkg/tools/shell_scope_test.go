package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecSessionAuthorization(t *testing.T) {
	tool, err := NewExecTool(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	tool.sessionManager = NewSessionManager()
	t.Cleanup(tool.sessionManager.Stop)
	a := WithToolSessionContext(WithToolContext(context.Background(), "cli", "one"), "a", "a:one", nil)
	b := WithToolSessionContext(WithToolContext(context.Background(), "telegram", "two"), "b", "b:two", nil)
	tool.sessionManager.Add(
		&ProcessSession{
			ID:           "owned",
			owner:        tool.processOwner(a),
			Status:       "done",
			outputBuffer: bytes.NewBufferString("private"),
		},
	)
	for _, action := range []string{"poll", "read", "write", "kill", "send-keys"} {
		result := tool.Execute(b, map[string]any{"action": action, "sessionId": "owned", "data": "x", "keys": "enter"})
		if !result.IsError {
			t.Fatalf("foreign %s allowed", action)
		}
	}
	if strings.Contains(tool.Execute(b, map[string]any{"action": "list"}).ForLLM, "owned") {
		t.Fatal("foreign session listed")
	}
	if result := tool.Execute(
		a,
		map[string]any{"action": "read", "sessionId": "owned"},
	); result.IsError ||
		!strings.Contains(result.ForLLM, "private") {
		t.Fatalf("owner read failed: %+v", result)
	}
	tool.allowRemote = false
	for _, action := range []string{"run", "list", "read", "write", "kill", "send-keys"} {
		if !tool.Execute(b, map[string]any{"action": action, "sessionId": "owned"}).IsError {
			t.Fatalf("remote %s allowed", action)
		}
	}
}
