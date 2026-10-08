// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"os"
	"strings"
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
		t.Logf(
			"NOTE: %d tokens > NFR-007's 300-token target for heartbeat, even with an EMPTY workspace and the real compact tool transform applied — this is a genuine framework-overhead gap, not workspace content.",
			total,
		)
	}
}

// TestFocusHeartbeatWindow_RealisticWorkspaceFitsTotalPromptBudget guards the
// operational budget, not merely framework overhead. A focus window is not
// actually lightweight if it still injects every workspace document in full.
func TestFocusHeartbeatWindow_RealisticWorkspaceFitsTotalPromptBudget(t *testing.T) {
	tmpDir := setupWorkspace(t, map[string]string{
		"AGENT.md": `---
name: Sensores
description: Irrigation and sensor monitoring agent.
---
# Agent
You are Sensores, the irrigation and sensor monitoring agent.

` + strings.Repeat("Detailed operating instruction for interactive work. ", 90),
		"SOUL.md":          strings.Repeat("Stable personality preference. ", 35),
		"USER.md":          strings.Repeat("Long-lived user preference. ", 40),
		"memory/MEMORY.md": strings.Repeat("Relevant long-term fact. ", 12),
	})
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Focus = config.FocusConfig{Enabled: true}
	provider := &turnProfileCaptureProvider{}
	al := newTurnProfileAgentLoop(t, cfg, provider)
	agent := al.GetRegistry().GetDefaultAgent()

	_, err := al.runAgentLoop(context.Background(), agent, processOptions{
		SessionKey:  "heartbeat-realistic-budget",
		UserMessage: strings.Repeat("Check sensors and report only actionable changes. ", 10),
		Origin:      OriginHeartbeat,
		NoHistory:   true,
	})
	if err != nil {
		t.Fatalf("runAgentLoop() error = %v", err)
	}

	messageTokens := 0
	for _, message := range provider.messages {
		messageTokens += EstimateMessageTokens(message)
	}
	compactDefs, err := common.TransformToolDefinitionsWithOptions(
		provider.tools, common.ToolSchemaTransformCompact, common.CompactSchemaOptions{},
	)
	if err != nil {
		t.Fatalf("TransformToolDefinitionsWithOptions(compact) error = %v", err)
	}
	total := messageTokens + tokenizer.EstimateToolDefsTokens(compactDefs)
	t.Logf("realistic heartbeat total=%d (messages=%d compact_tools=%d)",
		total, messageTokens, tokenizer.EstimateToolDefsTokens(compactDefs))

	if total > 900 {
		t.Fatalf("realistic heartbeat prompt = %d tokens, want <=900", total)
	}
}
