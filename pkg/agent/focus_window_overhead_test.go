// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
	"github.com/andre25costa-code/kuromatsu/pkg/tokenizer"
)

// focusWindowOverheadBudget is NFR-006's per-window framework-overhead
// ceiling (system prompt + compact-transformed tool defs), in tokens.
// Windows not listed here have no NFR-006 target yet -- their numbers are
// still measured and logged for S39/S40, just not compared to anything.
var focusWindowOverheadBudget = map[string]int{
	"chat":      450,
	"heartbeat": 300,
	"full":      1500,
}

// TestFocusWindows_RealFrameworkOverhead is Trilho F's O4/KR4.1: "nunca
// alegar eficiência sem número real" as an institutional gate, generalized
// from TestFocusHeartbeatWindow_RealFrameworkOverhead (which proved this
// method for the heartbeat window alone) to all nine of
// config.DefaultFocusWindows(). Each window is measured with an EMPTY
// workspace (setupWorkspace(t, nil): no AGENT/SOUL/USER/MEMORY content) so
// the captured system prompt is framework overhead only, and with the real
// compact tool-schema transform (common.TransformToolDefinitionsWithOptions
// -- the exact function pkg/providers/tool_schema_transform.go's wrapper
// calls before every real Chat()), not an estimate of it.
//
// A budget miss is logged (t.Logf), never t.Fatal: the point of this test
// is to produce the real number for every window, not to gate a build on a
// target that was never measured before this test existed.
func TestFocusWindows_RealFrameworkOverhead(t *testing.T) {
	windows := config.DefaultFocusWindows()
	names := make([]string, 0, len(windows))
	for name := range windows {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			tmpDir := setupWorkspace(t, nil)
			defer os.RemoveAll(tmpDir)

			cfg := config.DefaultConfig()
			cfg.Agents.Defaults.Workspace = tmpDir
			cfg.Agents.Defaults.Focus = config.FocusConfig{Enabled: true}
			provider := &turnProfileCaptureProvider{}
			al := newTurnProfileAgentLoop(t, cfg, provider)
			agent := al.GetRegistry().GetDefaultAgent()

			// heartbeat/cron are origin-routed in production (no inline
			// tag involved); every other window is reached the way /foco
			// reaches it -- an explicit "[foco:<window>]" tag, which the
			// router honors ahead of keyword/regex rules (ADR-014 point 1)
			// and strips before it reaches the prompt.
			origin := OriginUser
			message := "[foco:" + name + "] test message"
			noHistory := false
			switch name {
			case "heartbeat":
				origin = OriginHeartbeat
				message = "heartbeat check"
				noHistory = true
			case "cron":
				origin = OriginCron
				message = "cron check"
				noHistory = true
			}

			_, err := al.runAgentLoop(context.Background(), agent, processOptions{
				SessionKey:      "focus-overhead-" + name,
				UserMessage:     message,
				DefaultResponse: defaultResponse,
				Origin:          origin,
				NoHistory:       noHistory,
			})
			if err != nil {
				t.Fatalf("runAgentLoop() error = %v", err)
			}
			if len(provider.messages) == 0 {
				t.Fatal("no messages captured by provider")
			}

			systemPrompt := provider.messages[0].Content
			systemTokens := estimatePromptTokens(systemPrompt)

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

			total := systemTokens + compactToolTokens
			t.Logf("window=%-9s system=%4d tools_raw=%4d tools_compact=%4d TOTAL=%4d tools=%v",
				name, systemTokens, rawToolTokens, compactToolTokens, total, toolNames)

			if budget, ok := focusWindowOverheadBudget[name]; ok && total > budget {
				t.Logf("NOTE: window %q framework overhead = %d tokens > NFR-006 budget %d, even with an EMPTY workspace and the real compact transform applied -- genuine framework-overhead gap, not workspace content.",
					name, total, budget)
			}
		})
	}
}
