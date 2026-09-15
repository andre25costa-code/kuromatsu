// PicoClaw - Ultra-lightweight personal AI agent

package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Focus memory modes (ADR-014 point 3 / FR-015): independent of
// TurnProfileMode. "default" keeps today's MEMORY.md + recent-daily-notes
// behavior, "core" trims memory context to MEMORY.md only, "off" drops
// memory context entirely for the window.
const (
	FocusMemoryDefault = "default"
	FocusMemoryCore    = "core"
	FocusMemoryOff     = "off"
)

// DefaultFocusWindow is the fallback window used when FocusConfig.Default is
// empty (ADR-014/S16).
const DefaultFocusWindow = "chat"

// FullFocusWindow is the ceiling window escalation targets by default
// (ADR-014 point 5; also the target of "/foco full").
const FullFocusWindow = "full"

// FocusTriggers lists the deterministic, 0-token signals
// (pkg/routing/focus.go) that route a user message into a window: keyword
// substrings (matched case/accent-folded) and regexes (matched
// case-insensitively). Either list may be empty.
type FocusTriggers struct {
	Keywords []string `json:"keywords,omitempty"`
	Regex    []string `json:"regex,omitempty"`
}

// FocusWindow restricts tools/skills/history/memory/prompt-mode for one
// focus window (ADR-014 point 2). Config windows are merged over
// DefaultFocusWindows() by name (FocusConfig.mergedWindows) — a configured
// window entry fully replaces the corresponding built-in entry, it is not
// deep-merged field by field.
type FocusWindow struct {
	Tools        TurnProfileBlock `json:"tools,omitempty"`
	Skills       TurnProfileBlock `json:"skills,omitempty"`
	SystemPrompt TurnProfileMode  `json:"system_prompt,omitempty"`
	History      TurnProfileMode  `json:"history,omitempty"`
	// Memory is one of FocusMemoryDefault/Core/Off (case-insensitive);
	// anything else falls back to FocusMemoryDefault.
	Memory   string        `json:"memory,omitempty"`
	Triggers FocusTriggers `json:"triggers,omitempty"`
	// StickyTurns overrides FocusConfig.StickyTurns for this window. nil
	// means "use the config-level default"; 0 means "never sticky".
	StickyTurns *int `json:"sticky_turns,omitempty"`
	// EscalateTo names the window escalation targets when a globally-known
	// tool is requested outside this window (ADR-014 point 5). nil means
	// "full" (the conventional ceiling); a pointer to "" means "never
	// escalate" (used by full/heartbeat/cron themselves).
	EscalateTo *string `json:"escalate_to,omitempty"`
	// NeedsTime marks windows sensitive to time-of-day; see
	// EffectiveTurnProfile.NeedsTime.
	NeedsTime bool `json:"needs_time,omitempty"`
}

func (w FocusWindow) effectiveStickyTurns(fallback int) int {
	if w.StickyTurns != nil {
		return *w.StickyTurns
	}
	return fallback
}

func (w FocusWindow) effectiveEscalateTo() string {
	if w.EscalateTo == nil {
		return FullFocusWindow
	}
	return strings.ToLower(strings.TrimSpace(*w.EscalateTo))
}

func (w FocusWindow) effectiveMemoryMode() string {
	switch strings.ToLower(strings.TrimSpace(w.Memory)) {
	case FocusMemoryCore:
		return FocusMemoryCore
	case FocusMemoryOff:
		return FocusMemoryOff
	default:
		return FocusMemoryDefault
	}
}

// effectiveSystemPromptMode defaults an unset SystemPrompt to "compact"
// (ADR-014 point 3: "SystemPrompt (janelas -> compact)") — every focus
// window's whole point is to reduce framework overhead, and the compact
// identity doesn't restrict tools/skills availability at all (that's
// ToolsMode/SkillsMode's job), so there's no reason for any window,
// including "full", to default to the verbose one. A window can still opt
// back into the full prompt explicitly with "default".
func (w FocusWindow) effectiveSystemPromptMode() TurnProfileMode {
	raw := strings.ToLower(strings.TrimSpace(string(w.SystemPrompt)))
	if raw == "" {
		return TurnProfileModeCompact
	}
	switch TurnProfileMode(raw) {
	case TurnProfileModeOff, TurnProfileModeCompact, TurnProfileModeCustom, TurnProfileModeDefault:
		return TurnProfileMode(raw)
	default:
		return TurnProfileModeCompact
	}
}

func (w FocusWindow) effectiveHistoryMode() TurnProfileMode {
	if TurnProfileMode(strings.ToLower(strings.TrimSpace(string(w.History)))) == TurnProfileModeOff {
		return TurnProfileModeOff
	}
	return TurnProfileModeDefault
}

// FocusEscalationConfig controls the bounded tool-escalation mechanism
// (ADR-014 point 5 / FR-014 AC-014-6/7).
type FocusEscalationConfig struct {
	// Enabled defaults to true (nil == enabled).
	Enabled *bool `json:"enabled,omitempty"`
	// MaxPerTurn caps how many times a single turn may escalate its window.
	// Defaults to 1 when <= 0.
	MaxPerTurn int `json:"max_per_turn,omitempty"`
}

func (e FocusEscalationConfig) effectiveEnabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

func (e FocusEscalationConfig) effectiveMaxPerTurn() int {
	if e.MaxPerTurn > 0 {
		return e.MaxPerTurn
	}
	return 1
}

// FocusConfig is the top-level "janelas de foco" configuration
// (AgentDefaults.Focus, ADR-014). With Enabled=false (the default), nothing
// in the focus/reflex feature touches the turn — see S16 and AC-014-9.
type FocusConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// Default is the window used when the router finds no other signal.
	// Empty resolves to DefaultFocusWindow ("chat").
	Default string `json:"default,omitempty"`
	// StickyTurns is the config-level default sticky_turns used by windows
	// that don't set their own (FocusWindow.StickyTurns).
	StickyTurns int `json:"sticky_turns,omitempty"`
	// Origins maps a non-user turn origin ("heartbeat", "cron", "system",
	// "reflex") to the window it always resolves to, bypassing the
	// keyword/regex rules (which only make sense for user messages).
	Origins map[string]string `json:"origins,omitempty"`
	// Windows overrides/extends DefaultFocusWindows() by name.
	Windows    map[string]FocusWindow `json:"windows,omitempty"`
	Escalation FocusEscalationConfig  `json:"escalation,omitempty"`
}

func ptrString(s string) *string { return &s }
func ptrInt(i int) *int          { return &i }

// DefaultFocusWindows returns the built-in window table from ADR-014/S16.
// FocusConfig.Windows entries are merged on top of this table by name.
func DefaultFocusWindows() map[string]FocusWindow {
	return map[string]FocusWindow{
		"chat": {
			Tools:  TurnProfileBlock{Mode: TurnProfileModeOff},
			Skills: TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory: FocusMemoryCore,
		},
		"files": {
			Tools: TurnProfileBlock{
				Mode:  TurnProfileModeCustom,
				Allow: []string{"read_file", "list_dir", "write_file", "edit_file", "append_file"},
			},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory:      FocusMemoryCore,
			StickyTurns: ptrInt(3),
			Triggers: FocusTriggers{
				Keywords: []string{
					"arquivo", "pasta", "diretorio", "leia", "escreva", "edite", "crie", "salve",
					"file", "folder",
				},
				Regex: []string{
					`(^|\s)(\.{0,2}/|~/)[\w./-]+`,
					`\.(md|txt|go|json|ya?ml|py|sh)\b`,
				},
			},
		},
		"shell": {
			Tools:       TurnProfileBlock{Mode: TurnProfileModeCustom, Allow: []string{"exec", "sysmon"}},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory:      FocusMemoryCore,
			StickyTurns: ptrInt(2),
			Triggers: FocusTriggers{
				Keywords: []string{
					"execute", "rode", "comando", "shell", "processo", "memoria", "cpu", "disco",
					"uptime", "run", "command",
				},
			},
		},
		"web": {
			Tools:       TurnProfileBlock{Mode: TurnProfileModeCustom, Allow: []string{"web_search", "web_fetch"}},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory:      FocusMemoryCore,
			StickyTurns: ptrInt(2),
			Triggers: FocusTriggers{
				Keywords: []string{"pesquise", "busca", "procure", "noticia", "site", "url", "search", "fetch"},
				Regex:    []string{`https?://`},
			},
		},
		"schedule": {
			Tools:       TurnProfileBlock{Mode: TurnProfileModeCustom, Allow: []string{"cron", "message"}},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory:      FocusMemoryCore,
			StickyTurns: ptrInt(1),
			NeedsTime:   true,
			Triggers: FocusTriggers{
				Keywords: []string{
					"lembre", "lembrete", "agende", "cron", "todo dia", "amanha", "daqui a",
					"minutos", "remind", "schedule",
				},
				Regex: []string{`\b(as|às)\s?\d{1,2}(:\d{2})?\b`},
			},
		},
		"memory": {
			Tools: TurnProfileBlock{
				Mode:  TurnProfileModeCustom,
				Allow: []string{"read_file", "edit_file", "append_file", "write_file"},
			},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory:      FocusMemoryDefault,
			StickyTurns: ptrInt(1),
			Triggers: FocusTriggers{
				Keywords: []string{
					"memorize", "anote", "guarde", "lembre-se", "esqueca", "memory.md", "remember",
				},
			},
		},
		"full": {
			Tools:       TurnProfileBlock{Mode: TurnProfileModeDefault},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeDefault},
			Memory:      FocusMemoryDefault,
			StickyTurns: ptrInt(2),
			EscalateTo:  ptrString(""),
		},
		"heartbeat": {
			Tools:       TurnProfileBlock{Mode: TurnProfileModeCustom, Allow: []string{"message", "sysmon", "cron"}},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory:      FocusMemoryCore,
			History:     TurnProfileModeOff,
			StickyTurns: ptrInt(0),
			NeedsTime:   true,
			EscalateTo:  ptrString(""),
		},
		"cron": {
			Tools:       TurnProfileBlock{Mode: TurnProfileModeCustom, Allow: []string{"exec", "message"}},
			Skills:      TurnProfileBlock{Mode: TurnProfileModeOff},
			Memory:      FocusMemoryCore,
			History:     TurnProfileModeOff,
			StickyTurns: ptrInt(0),
			NeedsTime:   true,
			EscalateTo:  ptrString(""),
		},
	}
}

// MergedWindows returns the merged window table (DefaultFocusWindows()
// overridden entry-by-entry by FocusConfig.Windows). Exported so pkg/agent
// can build a pkg/routing.FocusRouterConfig from it (pkg/routing never
// imports pkg/config — see routing/focus.go's own doc comment).
func (f FocusConfig) MergedWindows() map[string]FocusWindow {
	return f.mergedWindows()
}

func (f FocusConfig) mergedWindows() map[string]FocusWindow {
	merged := DefaultFocusWindows()
	for name, w := range f.Windows {
		merged[strings.ToLower(strings.TrimSpace(name))] = w
	}
	return merged
}

// DefaultFocusOrigins returns the built-in origin->window mapping (ADR-014
// point 7 / S16 step 7): the "heartbeat" and "cron" turn origins
// (routing.FocusInput.Origin / pkg/agent's OriginHeartbeat/OriginCron)
// resolve to the eponymous built-in windows (DefaultFocusWindows) without
// requiring any focus.origins config. FocusConfig.Origins entries are
// merged on top of this by key (MergedOrigins) — a configured origin
// overrides the built-in one; an explicit empty window ("") disables it.
func DefaultFocusOrigins() map[string]string {
	return map[string]string{
		"heartbeat": "heartbeat",
		"cron":      "cron",
	}
}

// MergedOrigins returns the merged origin table (DefaultFocusOrigins()
// overridden entry-by-entry by FocusConfig.Origins). Exported for the same
// reason as MergedWindows: pkg/agent builds a pkg/routing.FocusRouterConfig
// from it without pkg/routing ever importing pkg/config.
func (f FocusConfig) MergedOrigins() map[string]string {
	return f.mergedOrigins()
}

func (f FocusConfig) mergedOrigins() map[string]string {
	merged := DefaultFocusOrigins()
	for origin, window := range f.Origins {
		origin = strings.ToLower(strings.TrimSpace(origin))
		window = strings.ToLower(strings.TrimSpace(window))
		if origin == "" {
			continue
		}
		if window == "" {
			delete(merged, origin)
			continue
		}
		merged[origin] = window
	}
	return merged
}

func (f FocusConfig) effectiveDefault() string {
	if name := strings.ToLower(strings.TrimSpace(f.Default)); name != "" {
		return name
	}
	return DefaultFocusWindow
}

// ResolveWindow resolves a focus window name into an EffectiveTurnProfile
// carrying the window's tools/skills/history restrictions plus the
// focus-specific fields (Window, MemoryMode, NeedsTime, EscalateTo). An
// empty name resolves FocusConfig.Default (or DefaultFocusWindow). ok is
// false when the (possibly defaulted) name is not a configured window.
func (f FocusConfig) ResolveWindow(name string) (EffectiveTurnProfile, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = f.effectiveDefault()
	}
	w, ok := f.mergedWindows()[name]
	if !ok {
		return EffectiveTurnProfile{}, false
	}

	return EffectiveTurnProfile{
		Enabled:          true,
		Window:           name,
		HistoryMode:      w.effectiveHistoryMode(),
		SystemPromptMode: w.effectiveSystemPromptMode(),
		SkillsMode:       w.Skills.Mode.Effective(),
		ToolsMode:        w.Tools.Mode.Effective(),
		AllowedSkills:    cleanStringList(w.Skills.Allow),
		AllowedTools:     cleanStringList(w.Tools.Allow),
		MemoryMode:       w.effectiveMemoryMode(),
		NeedsTime:        w.NeedsTime,
		EscalateTo:       w.effectiveEscalateTo(),
	}, true
}

// EffectiveStickyTurns returns how many further messages should stay pinned
// to name after it was resolved, falling back to FocusConfig.StickyTurns
// when the window doesn't set its own.
func (f FocusConfig) EffectiveStickyTurns(name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	w, ok := f.mergedWindows()[name]
	if !ok {
		return 0
	}
	return w.effectiveStickyTurns(f.StickyTurns)
}

// EscalationEnabled reports whether bounded tool escalation is active.
func (f FocusConfig) EscalationEnabled() bool {
	return f.Escalation.effectiveEnabled()
}

// MaxEscalationsPerTurn returns the configured escalation cap (>=1).
func (f FocusConfig) MaxEscalationsPerTurn() int {
	return f.Escalation.effectiveMaxPerTurn()
}

// ListWindowNames returns the sorted, merged window names (built-ins +
// config overrides) — used by the /foco command to list what's available.
func (f FocusConfig) ListWindowNames() []string {
	merged := f.mergedWindows()
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateFocus validates AgentDefaults.Focus. It is a strict no-op when
// focus.enabled is false, so a config that never turns focus on can never
// fail to boot because of it (mirrors ValidateTurnProfile's own
// disabled-is-a-noop contract).
func (c *Config) ValidateFocus() error {
	if c == nil {
		return nil
	}
	return validateFocusConfig(c.Agents.Defaults.Focus)
}

func validateFocusConfig(f FocusConfig) error {
	if !f.Enabled {
		return nil
	}

	windows := f.mergedWindows()
	if _, ok := windows[f.effectiveDefault()]; !ok {
		return fmt.Errorf("focus.default %q is not a configured window", f.Default)
	}

	for origin, window := range f.Origins {
		window = strings.ToLower(strings.TrimSpace(window))
		if window == "" {
			continue
		}
		if _, ok := windows[window]; !ok {
			return fmt.Errorf("focus.origins[%q] references unknown window %q", origin, window)
		}
	}

	for name, w := range windows {
		if err := validateTurnProfileBlock("focus.windows."+name+".tools", w.Tools, true); err != nil {
			return err
		}
		if err := validateTurnProfileBlock("focus.windows."+name+".skills", w.Skills, true); err != nil {
			return err
		}
		if w.SystemPrompt != "" {
			if err := validateTurnProfileBlock(
				"focus.windows."+name+".system_prompt",
				TurnProfileBlock{Mode: w.SystemPrompt},
				false,
			); err != nil {
				return err
			}
		}
		if escalateTo := w.effectiveEscalateTo(); escalateTo != "" {
			if _, ok := windows[escalateTo]; !ok {
				return fmt.Errorf("focus.windows.%s.escalate_to references unknown window %q", name, escalateTo)
			}
		}
		for _, pattern := range w.Triggers.Regex {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("focus.windows.%s.triggers.regex %q: %w", name, pattern, err)
			}
		}
	}

	return nil
}

