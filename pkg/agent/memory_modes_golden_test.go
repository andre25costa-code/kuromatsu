package agent

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// AC-022-4: the memory modes production runs today (off, core, default and
// the unset mode) must render byte-identical prompts after FR-022 phase 2
// adds "retrieved". The goldens were recorded before that change.
func TestMemoryModes_StaticPromptUnchanged(t *testing.T) {
	ws := setupWorkspace(t, map[string]string{
		"AGENT.md":         "# Agent\nGolden test agent.",
		"memory/MEMORY.md": "# Memória\n\n- [Ana] prefere respostas curtas\n- nunca apague backups sem perguntar\n- o rack fica na sala\n",
	})
	defer os.RemoveAll(ws)

	for _, mode := range []string{"", config.FocusMemoryDefault, config.FocusMemoryCore, config.FocusMemoryOff} {
		name := mode
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			cb := NewContextBuilder(ws)
			req := PromptBuildRequest{
				CompactSystemPrompt: true,
				MemoryMode:          mode,
				CurrentMessage:      "quando roda o backup?",
			}
			prompt, _ := cb.buildSystemPromptForRequest(req)
			// The temp workspace path differs on every run.
			prompt = strings.ReplaceAll(prompt, ws, "{{WORKSPACE}}")
			golden := filepath.Join("testdata", "memory_modes", name+".golden")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(prompt), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if prompt != string(want) {
				t.Fatalf(
					"static prompt for memory mode %q changed:\n--- got ---\n%s\n--- want ---\n%s",
					mode,
					prompt,
					want,
				)
			}

			msgs := cb.BuildMessagesFromPrompt(req)
			last := msgs[len(msgs)-1]
			if last.Role != "user" || last.Content != req.CurrentMessage {
				t.Fatalf("user message altered in mode %q: %q", mode, last.Content)
			}
			if _, err := os.Stat(filepath.Join(ws, "memory", "atoms.db")); !os.IsNotExist(err) {
				t.Fatalf("mode %q created memory/atoms.db; only retrieved may open it", mode)
			}
		})
	}
}
