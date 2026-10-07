package agent

import (
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
)

func resolveTurnProfileOptions(cfg *config.Config, opts processOptions) (processOptions, error) {
	if cfg == nil {
		return opts, nil
	}
	profile, ok, err := cfg.Agents.Defaults.ResolveTurnProfile()
	if err != nil {
		return opts, err
	}
	if !ok {
		return opts, nil
	}
	opts.TurnProfile = profile
	if profile.HistoryMode == config.TurnProfileModeOff {
		opts.NoHistory = true
		opts.EnableSummary = false
	}
	return opts, nil
}

func turnProfileSystemPromptOff(profile config.EffectiveTurnProfile) bool {
	return profile.Enabled && profile.SystemPromptMode == config.TurnProfileModeOff
}

// turnProfileSystemPromptCompact reports whether the active profile selects
// the compact framework identity (ADR-014 point 3 / FR-015 AC-015-1). A
// plain, non-focus AgentDefaults.TurnProfile can also set this directly via
// system_prompt.mode: "compact" — focus windows aren't the only source.
func turnProfileSystemPromptCompact(profile config.EffectiveTurnProfile) bool {
	return profile.Enabled && profile.SystemPromptMode == config.TurnProfileModeCompact
}

func turnProfileSkillsOff(profile config.EffectiveTurnProfile) bool {
	return profile.Enabled && profile.SkillsMode == config.TurnProfileModeOff
}

func turnProfileCustomSkills(profile config.EffectiveTurnProfile) bool {
	return profile.Enabled && profile.SkillsMode == config.TurnProfileModeCustom
}

func turnProfileHasCallableTools(
	profile config.EffectiveTurnProfile,
	defs []providers.ToolDefinition,
) bool {
	if !profile.Enabled {
		return true
	}
	return len(filterToolsByTurnProfile(defs, profile)) > 0
}

func turnProfileToolAllowed(profile config.EffectiveTurnProfile, name string) bool {
	if !profile.Enabled {
		return true
	}
	switch profile.ToolsMode {
	case config.TurnProfileModeOff:
		return false
	case config.TurnProfileModeCustom:
		allowed := cleanAllowedSet(profile.AllowedTools)
		if len(allowed) == 0 {
			return false
		}
		_, ok := allowed[strings.ToLower(strings.TrimSpace(name))]
		return ok
	default:
		return true
	}
}

func toolUseSystemPromptRule() string {
	return "**ALWAYS use tools** - When you need to perform an action (schedule reminders, send messages, execute commands, etc.), you MUST call the appropriate tool. Do NOT just say you'll do it or pretend to do it."
}

func filterNamesByTurnProfile(names []string, allowed []string) []string {
	if len(names) == 0 {
		return nil
	}
	allowedSet := cleanAllowedSet(allowed)
	if len(allowedSet) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := allowedSet[strings.ToLower(strings.TrimSpace(name))]; ok {
			out = append(out, name)
		}
	}
	return out
}

func filterToolsByTurnProfile(
	defs []providers.ToolDefinition,
	profile config.EffectiveTurnProfile,
) []providers.ToolDefinition {
	if !profile.Enabled {
		return defs
	}
	switch profile.ToolsMode {
	case config.TurnProfileModeOff:
		return nil
	case config.TurnProfileModeCustom:
		allowed := cleanAllowedSet(profile.AllowedTools)
		if len(allowed) == 0 {
			return nil
		}
		filtered := make([]providers.ToolDefinition, 0, len(defs))
		for _, def := range defs {
			if _, ok := allowed[strings.ToLower(strings.TrimSpace(def.Function.Name))]; ok {
				filtered = append(filtered, def)
			}
		}
		return filtered
	default:
		return defs
	}
}

func cleanAllowedSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}
