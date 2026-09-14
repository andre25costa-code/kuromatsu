// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// chatPinKey derives the /agent pin scope from an inbound context: channel
// + chat, lower-cased. Deliberately coarser than a session key (which is
// itself agent-scoped -- session allocation keys off the already-resolved
// AgentID) -- the pin must be resolvable BEFORE an agent, and therefore a
// session key, is chosen, so it cannot key off the session key.
func chatPinKey(ctx bus.InboundContext) string {
	channel := strings.ToLower(strings.TrimSpace(ctx.Channel))
	chatID := strings.ToLower(strings.TrimSpace(ctx.ChatID))
	if channel == "" || chatID == "" {
		return ""
	}
	return channel + "|" + chatID
}

// agentPinsFilePath is $KUROMATSU_HOME/run/agent-pins.json -- same
// directory as the runstate engine's run/state file (pkg/runstate).
func agentPinsFilePath() string {
	return filepath.Join(config.GetHome(), "run", "agent-pins.json")
}

// loadAgentPins reads the persisted pins file. A missing or unreadable
// file is not an error (compatibility with a fresh install / no pins ever
// set yet) -- same stance as state.Manager.load().
func loadAgentPins() map[string]string {
	data, err := os.ReadFile(agentPinsFilePath())
	if err != nil {
		return map[string]string{}
	}
	var pins map[string]string
	if err := json.Unmarshal(data, &pins); err != nil {
		logger.WarnCF("agent", "failed to parse agent-pins.json, starting empty", map[string]any{
			"error": err.Error(),
		})
		return map[string]string{}
	}
	if pins == nil {
		pins = map[string]string{}
	}
	return pins
}

// saveAgentPins persists al.agentPins to disk atomically. Best-effort: a
// write failure is logged, not returned, since a pin still works for the
// rest of this process's life either way -- only durability across a
// restart is at stake.
func (al *AgentLoop) saveAgentPins() {
	pins := make(map[string]string)
	al.agentPins.Range(func(k, v any) bool {
		key, _ := k.(string)
		val, _ := v.(string)
		if key != "" && val != "" {
			pins[key] = val
		}
		return true
	})
	data, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		logger.WarnCF("agent", "failed to marshal agent pins", map[string]any{"error": err.Error()})
		return
	}
	if err := fileutil.WriteFileAtomic(agentPinsFilePath(), data, 0o600); err != nil {
		logger.WarnCF("agent", "failed to persist agent pins", map[string]any{"error": err.Error()})
	}
}

// pinnedAgentID returns the agent pinned for key, if any.
func (al *AgentLoop) pinnedAgentID(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	v, ok := al.agentPins.Load(key)
	if !ok {
		return "", false
	}
	id, _ := v.(string)
	return id, id != ""
}

// pinAgentID pins key to agentID and persists the change.
func (al *AgentLoop) pinAgentID(key, agentID string) {
	if key == "" || agentID == "" {
		return
	}
	al.agentPins.Store(key, agentID)
	al.saveAgentPins()
}

// unpinAgentID clears any pin for key and persists the change.
func (al *AgentLoop) unpinAgentID(key string) {
	if key == "" {
		return
	}
	if _, existed := al.agentPins.LoadAndDelete(key); existed {
		al.saveAgentPins()
	}
}
