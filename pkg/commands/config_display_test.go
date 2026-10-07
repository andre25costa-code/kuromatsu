package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestShowConfigReportsProvenanceWithoutCredentials(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourcePath = "/var/lib/kuromatsu/config.json"
	cfg.Agents.Defaults.Workspace = "/var/lib/kuromatsu/workspace"
	cfg.ModelList[0].SetAPIKey("do-not-display-this-secret")
	var reply string
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{Config: cfg})
	ex.Execute(
		context.Background(),
		Request{Text: "/show config", Reply: func(text string) error { reply = text; return nil }},
	)
	if !strings.Contains(reply, cfg.SourcePath) || !strings.Contains(reply, cfg.Agents.Defaults.Workspace) ||
		strings.Contains(reply, "do-not-display-this-secret") {
		t.Fatalf("invalid provenance reply: %q", reply)
	}
}
