// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

// compiledReflex pairs a ReflexConfig with its compiled regex.
type compiledReflex struct {
	cfg config.ReflexConfig
	re  *regexp.Regexp
}

// reflexRuntime holds the compiled AgentDefaults.Reflexes list for one
// AgentLoop generation (ADR-014 point 6 / FR-013). A nil *reflexRuntime
// (or one with zero compiled entries) means "no reflexes configured" —
// tryReflex is then a complete no-op, matching AC-013-5.
type reflexRuntime struct {
	reflexes []compiledReflex
}

// newReflexRuntime compiles cfg.Agents.Defaults.Reflexes once, or returns
// nil when the list is empty (or cfg is nil) — the default, zero-overhead
// state.
func newReflexRuntime(cfg *config.Config) *reflexRuntime {
	if cfg == nil || len(cfg.Agents.Defaults.Reflexes) == 0 {
		return nil
	}
	compiled := make([]compiledReflex, 0, len(cfg.Agents.Defaults.Reflexes))
	for _, r := range cfg.Agents.Defaults.Reflexes {
		re, err := regexp.Compile(r.Match)
		if err != nil {
			// ValidateReflexes should have already rejected this at boot;
			// never let a bad pattern here take the whole reflex feature
			// down — just skip this one entry.
			logger.WarnCF("agent", "Skipping reflex with invalid regex", map[string]any{
				"match": r.Match,
				"error": err.Error(),
			})
			continue
		}
		compiled = append(compiled, compiledReflex{cfg: r, re: re})
	}
	if len(compiled) == 0 {
		return nil
	}
	return &reflexRuntime{reflexes: compiled}
}

func (al *AgentLoop) reflexRuntimeSnapshot() *reflexRuntime {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.reflexes
}

// tryReflex checks msg against every compiled reflex in order and, on the
// first match, executes it (reply/command/exec) and returns its response
// without ever calling the LLM. matched is false when nothing matched (or
// no reflexes are configured), in which case the caller should fall
// through to the normal focus-router/LLM pipeline.
//
// The matched branch is wrapped in the Trilho C Reflex bit
// (runstate.Enter, ADR-016 point 7): reflexes are meant to be visible as a
// distinct, near-instant occupancy state, not silently invisible to
// heartbeat/cron scheduling decisions. A nil al.runstate
// (runstate.enabled=false, the default) makes this a no-op, matching
// AC-013-5 exactly (no reflexes configured, or runstate off, is byte-for-
// byte the pre-existing pipeline).
func (al *AgentLoop) tryReflex(
	ctx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
	opts *processOptions,
) (string, bool) {
	rr := al.reflexRuntimeSnapshot()
	if rr == nil {
		return "", false
	}

	message := strings.TrimSpace(msg.Content)
	for _, cr := range rr.reflexes {
		loc := cr.re.FindStringSubmatchIndex(message)
		if loc == nil {
			continue
		}

		release := al.rsEnter(runstate.Reflex)
		defer release()

		sender := strings.TrimSpace(msg.Sender.DisplayName)
		if sender == "" {
			sender = msg.SenderID
		}
		rendered := renderReflexTemplate(cr.re, cr.cfg.Template, message, loc, sender)

		var response string
		switch cr.cfg.Action {
		case config.ReflexActionExec:
			response = al.executeReflexExec(ctx, agent, msg, rendered)
		case config.ReflexActionCommand:
			response = al.executeReflexCommand(ctx, agent, msg, opts, rendered)
		default: // config.ReflexActionReply
			response = rendered
		}

		if cr.cfg.EffectivePersist() {
			al.persistReflexTurn(agent, opts.Dispatch.SessionKey, message, response)
		}

		logger.InfoCF("agent", "Reflex matched", map[string]any{
			"match":       cr.cfg.Match,
			"action":      string(cr.cfg.Action),
			"session_key": opts.Dispatch.SessionKey,
		})

		// AC-013-6/AC-019-3: a matched reflex never reaches runTurn (no
		// LLM call is ever made), so there is no KindAgentTurnEnd for
		// telemetry_bridge.go's OnRuntimeEvent to observe -- record
		// directly instead, unconditionally (independent of
		// EffectivePersist, which only controls session history).
		if tb := al.telemetrySnapshot(); tb != nil {
			tb.recordReflex(opts.Dispatch.SessionKey)
		}

		return response, true
	}

	return "", false
}

// renderReflexTemplate expands a matched reflex's regex capture groups
// ($1, $2, ${name}, ...) into template via regexp.Regexp.ExpandString, then
// substitutes the {{sender}}/{{time}} placeholders that aren't part of
// Go's regexp expansion syntax.
func renderReflexTemplate(re *regexp.Regexp, template, message string, matchLoc []int, sender string) string {
	expanded := re.ExpandString(nil, template, message, matchLoc)
	rendered := string(expanded)
	rendered = strings.ReplaceAll(rendered, "{{sender}}", sender)
	rendered = strings.ReplaceAll(rendered, "{{time}}", time.Now().Format("15:04"))
	return rendered
}

// executeReflexExec runs the rendered template as a shell command through
// the existing "exec" tool (Tools.ExecuteWithContext), inheriting the same
// deny patterns/restrict/timeouts any other exec call gets — no security
// bypass (AC-013-3).
func (al *AgentLoop) executeReflexExec(
	ctx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
	command string,
) string {
	if agent == nil || agent.Tools == nil {
		return "reflex exec unavailable: no tool registry"
	}
	result := agent.Tools.ExecuteWithContext(
		ctx,
		"exec",
		map[string]any{"action": "run", "command": command},
		msg.Channel,
		msg.ChatID,
		nil,
	)
	if result == nil {
		return ""
	}
	if content := strings.TrimSpace(result.ContentForLLM()); content != "" {
		return content
	}
	return result.ForUser
}

// executeReflexCommand runs the rendered template as a slash command
// through the existing handleCommand path (AC-013-2) — the same executor
// /help, /use, /context, etc. already go through.
func (al *AgentLoop) executeReflexCommand(
	ctx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
	opts *processOptions,
	commandLine string,
) string {
	commandMsg := msg
	commandMsg.Content = commandLine
	response, _ := al.handleCommand(ctx, commandMsg, agent, opts)
	return response
}

// persistReflexTurn records the user's message and the reflex's response
// in session history, mirroring what a normal LLM turn would persist
// (AC-013-4's default for reply/command).
func (al *AgentLoop) persistReflexTurn(agent *AgentInstance, sessionKey, userMessage, response string) {
	if agent == nil || agent.Sessions == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	agent.Sessions.AddFullMessage(sessionKey, userPromptMessage(userMessage, nil))
	if strings.TrimSpace(response) != "" {
		agent.Sessions.AddFullMessage(sessionKey, providers.Message{Role: "assistant", Content: response})
	}
}
