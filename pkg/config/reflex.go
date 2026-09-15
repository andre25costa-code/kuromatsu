// PicoClaw - Ultra-lightweight personal AI agent

package config

import (
	"fmt"
	"regexp"
	"strings"
)

// ReflexAction selects what a matched reflex does (pkg/agent/reflex.go,
// ADR-014 point 6 / FR-013). All three reuse existing, already-audited
// machinery — reflexes add no new capability, only a 0-token shortcut to it.
type ReflexAction string

const (
	// ReflexActionReply renders Template (with $1... capture groups,
	// {{sender}}, {{time}}) as the turn's response — no LLM call.
	ReflexActionReply ReflexAction = "reply"
	// ReflexActionExec runs the "exec" tool via Tools.ExecuteWithContext,
	// inheriting the same deny patterns/restrict/timeouts as a normal tool
	// call — no security bypass.
	ReflexActionExec ReflexAction = "exec"
	// ReflexActionCommand runs an existing slash command via handleCommand.
	ReflexActionCommand ReflexAction = "command"
)

// ReflexConfig is one deterministic, 0-token regex rule
// (AgentDefaults.Reflexes) checked before the focus router, for origin
// "user" messages only (FR-013). With no reflexes configured (the
// default), the pipeline is untouched — see AC-013-5.
type ReflexConfig struct {
	// Match is a regexp (Go syntax) tested against the raw message. Capture
	// groups feed $1, $2, ... substitutions in a "reply" Template.
	Match string `json:"match"`
	// Action is one of ReflexActionReply/Exec/Command.
	Action ReflexAction `json:"action"`
	// Template is the reply body for ReflexActionReply, or the slash
	// command line for ReflexActionCommand (e.g. "/context"). For
	// ReflexActionExec it is interpreted as the "command" argument passed
	// to the exec tool.
	Template string `json:"template,omitempty"`
	// Persist controls whether the matched user message and the reflex's
	// response are recorded in session history. nil defaults to true for
	// reply/command and false for exec (AC-013-4).
	Persist *bool `json:"persist,omitempty"`
	// Window optionally tags which focus window this reflex belongs to
	// (informational; reflexes run before the router regardless).
	Window string `json:"window,omitempty"`
}

// EffectivePersist reports whether a matched reflex should record the
// user's message and its own response in session history (AC-013-4).
// Exported because pkg/agent/reflex.go (a different package) calls it.
func (r ReflexConfig) EffectivePersist() bool {
	if r.Persist != nil {
		return *r.Persist
	}
	return r.Action != ReflexActionExec
}

func normalizeReflexAction(raw ReflexAction) (ReflexAction, error) {
	switch ReflexAction(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case ReflexActionReply:
		return ReflexActionReply, nil
	case ReflexActionExec:
		return ReflexActionExec, nil
	case ReflexActionCommand:
		return ReflexActionCommand, nil
	default:
		return "", fmt.Errorf("unsupported reflex action %q (supported: reply, exec, command)", raw)
	}
}

// ValidateReflexes validates AgentDefaults.Reflexes. An empty list (the
// default) is always valid — see AC-013-5.
func (c *Config) ValidateReflexes() error {
	if c == nil {
		return nil
	}
	return validateReflexConfigs(c.Agents.Defaults.Reflexes)
}

func validateReflexConfigs(reflexes []ReflexConfig) error {
	for i, r := range reflexes {
		if strings.TrimSpace(r.Match) == "" {
			return fmt.Errorf("reflexes[%d].match is required", i)
		}
		if _, err := regexp.Compile(r.Match); err != nil {
			return fmt.Errorf("reflexes[%d].match %q: %w", i, r.Match, err)
		}
		action, err := normalizeReflexAction(r.Action)
		if err != nil {
			return fmt.Errorf("reflexes[%d]: %w", i, err)
		}
		if strings.TrimSpace(r.Template) == "" {
			return fmt.Errorf("reflexes[%d].template is required for action %q", i, action)
		}
		if action == ReflexActionCommand && !strings.HasPrefix(strings.TrimSpace(r.Template), "/") {
			return fmt.Errorf(
				"reflexes[%d].template %q must start with \"/\" for action %q",
				i, r.Template, action,
			)
		}
	}
	return nil
}
