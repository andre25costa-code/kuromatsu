// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"os"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
	"github.com/andre25costa-code/kuromatsu/pkg/tokenizer"
)

// TestFocusHeartbeatWindow_RealFrameworkOverhead isolates the two hypotheses
// raised for the field gap found in S39 (2603 measured prompt_tokens for the
// "heartbeat" window vs. the ≤300-token framework-overhead target, NFR-007):
//
//  1. tool_schema_transform: compact isn't actually applied to the
//     heartbeat window's tools (message/sysmon/cron) — checked by comparing
//     EstimateToolDefsTokens on the raw defs the AgentLoop hands the
//     provider vs. the same defs run through the real compact transform
//     (common.TransformToolDefinitionsWithOptions, the exact function the
//     provider-level wrapper in pkg/providers/tool_schema_transform.go
//     calls before every real Chat()).
//  2. History:off isn't actually honored for heartbeat, or MEMORY.md/
//     workspace content leaks in regardless — checked with an EMPTY
//     workspace (setupWorkspace(t, nil): no AGENT/SOUL/USER/MEMORY files),
//     so the only thing left in the captured system prompt IS the
//     framework's own overhead.
func TestFocusHeartbeatWindow_RealFrameworkOverhead(t *testing.T) {
	tmpDir := setupWorkspace(t, nil)
	defer os.RemoveAll(tmpDir)

	// Start from config.DefaultConfig(), not a bare &config.Config{} literal:
	// that's what LoadConfig() actually layers the user's config.json onto
	// in production, and it's what turns on core tools (Tools.Cron.Enabled
	// etc. default to true there, false on a zero-value struct) — a bare
	// literal here would silently register zero tools and give a false
	// "compact schema shrinks nothing" reading, not a real one.
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Focus = config.FocusConfig{Enabled: true}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	agent := al.GetRegistry().GetDefaultAgent()

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:      "heartbeat",
		UserMessage:     "heartbeat check",
		DefaultResponse: defaultResponse,
		Origin:          OriginHeartbeat,
		NoHistory:       true,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}

	if len(provider.messages) == 0 {
		t.Fatal("no messages captured by provider")
	}
	systemPrompt := provider.messages[0].Content
	systemTokens := estimatePromptTokens(systemPrompt)

	if len(provider.tools) == 0 {
		t.Fatal("no tools captured by provider — heartbeat window resolved to zero tools, expected message/sysmon/cron")
	}
	rawToolTokens := tokenizer.EstimateToolDefsTokens(provider.tools)

	compactDefs, err := common.TransformToolDefinitionsWithOptions(
		provider.tools, common.ToolSchemaTransformCompact, common.CompactSchemaOptions{},
	)
	if err != nil {
		t.Fatalf("TransformToolDefinitionsWithOptions(compact) error = %v", err)
	}
	compactToolTokens := tokenizer.EstimateToolDefsTokens(compactDefs)

	toolNames := make([]string, 0, len(provider.tools))
	for _, td := range provider.tools {
		toolNames = append(toolNames, td.Function.Name)
	}

	t.Logf("heartbeat window, EMPTY workspace (isolates framework overhead only):")
	t.Logf("  system prompt: %d tokens (%d chars)", systemTokens, len(systemPrompt))
	t.Logf("  tools resolved: %v", toolNames)
	t.Logf("  tool defs, RAW (untransformed):     %d tokens", rawToolTokens)
	t.Logf("  tool defs, COMPACT (real transform): %d tokens", compactToolTokens)
	t.Logf("  TOTAL framework overhead (system + compact tools): %d tokens", systemTokens+compactToolTokens)
	t.Logf("  --- for reference ---")
	t.Logf("  full captured system prompt:\n%s", systemPrompt)

	// NFR-007's target for the heartbeat window specifically is <=300 tokens
	// of framework overhead. This does not Fatal on a miss — the point of
	// this test is to produce the real number for S39/BACKLOG, not to gate
	// a build on a target that was never measured before.
	total := systemTokens + compactToolTokens
	if total > 300 {
		t.Logf("NOTE: %d tokens > NFR-007's 300-token target for heartbeat, even with an EMPTY workspace and the real compact tool transform applied — this is a genuine framework-overhead gap, not workspace content.", total)
	}
}
