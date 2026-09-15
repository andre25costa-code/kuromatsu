package config

import (
	"fmt"
	"strings"
)

type TurnProfileMode string

const (
	TurnProfileModeDefault TurnProfileMode = "default"
	TurnProfileModeOff     TurnProfileMode = "off"
	TurnProfileModeCustom  TurnProfileMode = "custom"

	// TurnProfileModeCompact is valid only for the system_prompt block
	// (ADR-014 point 3 / FR-015): it selects the compact identity
	// (getIdentityCompact) and date-only dynamic context instead of the
	// full framework system prompt. Introduced for the focus/router work;
	// unrelated blocks (history/skills/tools) reject it — see
	// validateTurnProfileBlock.
	TurnProfileModeCompact TurnProfileMode = "compact"
)

type TurnProfileConfig struct {
	Enabled      bool             `json:"enabled"`
	History      TurnProfileBlock `json:"history,omitempty"`
	SystemPrompt TurnProfileBlock `json:"system_prompt,omitempty"`
	Skills       TurnProfileBlock `json:"skills,omitempty"`
	Tools        TurnProfileBlock `json:"tools,omitempty"`
}

type TurnProfileBlock struct {
	Mode  TurnProfileMode `json:"mode,omitempty"`
	Allow []string        `json:"allow,omitempty"`
}

type EffectiveTurnProfile struct {
	Enabled          bool
	HistoryMode      TurnProfileMode
	SystemPromptMode TurnProfileMode
	SkillsMode       TurnProfileMode
	ToolsMode        TurnProfileMode
	AllowedSkills    []string
	AllowedTools     []string

	// The fields below are populated only when this profile came from
	// focus-window resolution (FocusConfig.ResolveWindow, ADR-014/FR-014).
	// A profile resolved from the plain, static AgentDefaults.TurnProfile
	// (ResolveTurnProfile below) always leaves them at the zero value, which
	// is exactly what keeps focus.enabled=false byte-identical to today:
	// EscalateTo=="" means "never escalate", Window=="" means "no focus
	// window is active", MemoryMode=="" means "use the default memory
	// context", NeedsTime=false means "no [now: ...] timestamp is appended".

	// Window is the resolved focus window name (e.g. "chat", "files").
	Window string
	// MemoryMode overrides how much memory context is loaded for the prompt:
	// "" or "default" = MEMORY.md + recent daily notes (today's behavior),
	// "core" = MEMORY.md only, "off" = no memory context at all.
	MemoryMode string
	// NeedsTime marks windows sensitive to time-of-day (e.g. "schedule",
	// "heartbeat", "cron"): the agent layer appends "[now: HH:MM]" to the
	// assembled user message (never persisted, never in the system prompt)
	// instead of relying on the per-minute system-prompt timestamp that
	// would otherwise defeat prefix caching.
	NeedsTime bool
	// EscalateTo names the window to escalate to (bounded, ADR-014 point 5)
	// when the model requests a tool that exists in the global tool
	// registry but is outside the active window. Empty means "no
	// escalation" (e.g. the "full" window itself, or heartbeat/cron).
	EscalateTo string
}

func (m TurnProfileMode) Effective() TurnProfileMode {
	switch TurnProfileMode(strings.ToLower(strings.TrimSpace(string(m)))) {
	case "", TurnProfileModeDefault:
		return TurnProfileModeDefault
	case TurnProfileModeOff:
		return TurnProfileModeOff
	case TurnProfileModeCustom:
		return TurnProfileModeCustom
	default:
		return TurnProfileMode(strings.ToLower(strings.TrimSpace(string(m))))
	}
}

func (d *AgentDefaults) ResolveTurnProfile() (EffectiveTurnProfile, bool, error) {
	if d == nil {
		return EffectiveTurnProfile{}, false, nil
	}
	profile := d.TurnProfile
	if !profile.Enabled {
		return EffectiveTurnProfile{}, false, nil
	}
	if err := validateTurnProfile(profile); err != nil {
		return EffectiveTurnProfile{}, false, err
	}
	return EffectiveTurnProfile{
		Enabled:          true,
		HistoryMode:      profile.History.Mode.Effective(),
		SystemPromptMode: profile.SystemPrompt.Mode.Effective(),
		SkillsMode:       profile.Skills.Mode.Effective(),
		ToolsMode:        profile.Tools.Mode.Effective(),
		AllowedSkills:    cleanStringList(profile.Skills.Allow),
		AllowedTools:     cleanStringList(profile.Tools.Allow),
	}, true, nil
}

func (c *Config) ValidateTurnProfile() error {
	if c == nil {
		return nil
	}
	return validateTurnProfile(c.Agents.Defaults.TurnProfile)
}

func validateTurnProfile(profile TurnProfileConfig) error {
	if !profile.Enabled {
		return nil
	}
	if err := validateTurnProfileBlock("history", profile.History, false); err != nil {
		return err
	}
	if err := validateTurnProfileBlock("system_prompt", profile.SystemPrompt, false); err != nil {
		return err
	}
	if err := validateTurnProfileBlock("skills", profile.Skills, true); err != nil {
		return err
	}
	if err := validateTurnProfileBlock("tools", profile.Tools, true); err != nil {
		return err
	}
	return nil
}

func validateTurnProfileBlock(field string, block TurnProfileBlock, allowCustom bool) error {
	mode := block.Mode.Effective()
	switch mode {
	case TurnProfileModeDefault, TurnProfileModeOff:
		return nil
	case TurnProfileModeCompact:
		// "compact" only makes sense for system_prompt (ADR-014 point 3);
		// history/skills/tools blocks don't have a compact representation.
		// HasSuffix (not ==) because focus.go calls this with the longer
		// "focus.windows.<name>.system_prompt" field name.
		if strings.HasSuffix(field, "system_prompt") {
			return nil
		}
		return fmt.Errorf("turn_profile.%s.mode compact is not supported for this field", field)
	case TurnProfileModeCustom:
		if allowCustom {
			return nil
		}
		return fmt.Errorf("turn_profile.%s.mode custom is not supported in this version", field)
	default:
		return fmt.Errorf("turn_profile.%s.mode has unsupported mode %q", field, block.Mode)
	}
}

func cleanStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
