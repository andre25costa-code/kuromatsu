package integrationtools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// N31 (audit round 2): large MCP text results were saved under
// workspace/.artifacts/mcp and never removed. Writing a new artifact now
// prunes the ones older than mcpArtifactMaxAge; recent ones stay, since the
// agent may still read them by their [file:...] tag.
func TestPersistLargeTextArtifact_PrunesOldArtifacts(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".artifacts", "mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "srv_tool_old.txt")
	recent := filepath.Join(dir, "srv_tool_recent.txt")
	for p, age := range map[string]time.Duration{old: 2 * mcpArtifactMaxAge, recent: time.Hour} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}

	tool := NewMCPTool(nil, "srv", &mcp.Tool{Name: "tool"})
	tool.SetWorkspace(workspace)
	tool.SetMaxInlineTextRunes(10)
	if res := tool.persistLargeTextArtifact(strings.Repeat("a", 100)); res == nil || len(res.ArtifactTags) == 0 {
		t.Fatalf("new artifact not written: %+v", res)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("artifact older than %s was kept (err=%v)", mcpArtifactMaxAge, err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("recent artifact was removed: %v", err)
	}
}
