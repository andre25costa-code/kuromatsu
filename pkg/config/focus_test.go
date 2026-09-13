package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFocusConfig_DisabledIsNoop(t *testing.T) {
	cfg := &Config{}
	if err := cfg.ValidateFocus(); err != nil {
		t.Fatalf("ValidateFocus() error = %v, want nil when disabled", err)
	}
}

func TestFocusConfig_ResolveWindow_BuiltinChat(t *testing.T) {
	f := FocusConfig{Enabled: true}
	profile, ok := f.ResolveWindow("chat")
	if !ok {
		t.Fatal("ResolveWindow(chat) ok = false, want true")
	}
	if !profile.Enabled || profile.Window != "chat" {
		t.Fatalf("profile = %+v, want Enabled+Window=chat", profile)
	}
	if profile.ToolsMode != TurnProfileModeOff || profile.SkillsMode != TurnProfileModeOff {
		t.Fatalf("chat window modes = %+v, want tools/skills off", profile)
	}
	if profile.MemoryMode != FocusMemoryCore {
		t.Fatalf("chat window MemoryMode = %q, want core", profile.MemoryMode)
	}
	if profile.EscalateTo != FullFocusWindow {
		t.Fatalf("chat window EscalateTo = %q, want %q", profile.EscalateTo, FullFocusWindow)
	}
	if profile.NeedsTime {
		t.Fatal("chat window NeedsTime = true, want false")
	}
}

func TestFocusConfig_ResolveWindow_EmptyNameUsesDefault(t *testing.T) {
	f := FocusConfig{Enabled: true, Default: "files"}
	profile, ok := f.ResolveWindow("")
	if !ok || profile.Window != "files" {
		t.Fatalf("ResolveWindow(\"\") = (%+v, %v), want files window", profile, ok)
	}
	if len(profile.AllowedTools) == 0 {
		t.Fatal("files window AllowedTools empty, want the 5 file tools")
	}
}

func TestFocusConfig_ResolveWindow_UnknownNameFails(t *testing.T) {
	f := FocusConfig{Enabled: true}
	if _, ok := f.ResolveWindow("nonexistent"); ok {
		t.Fatal("ResolveWindow(nonexistent) ok = true, want false")
	}
}

func TestFocusConfig_ResolveWindow_FullHasNoEscalation(t *testing.T) {
	f := FocusConfig{Enabled: true}
	profile, ok := f.ResolveWindow("full")
	if !ok {
		t.Fatal("ResolveWindow(full) ok = false")
	}
	if profile.EscalateTo != "" {
		t.Fatalf("full window EscalateTo = %q, want empty (no further escalation)", profile.EscalateTo)
	}
	if profile.ToolsMode != TurnProfileModeDefault || profile.SkillsMode != TurnProfileModeDefault {
		t.Fatalf("full window modes = %+v, want default (unfiltered)", profile)
	}
}

// TestFocusConfig_ResolveWindow_SystemPromptDefaultsToCompact covers
// ADR-014 point 3: every window (not just the tool-restricted ones)
// defaults to the compact system prompt unless it explicitly opts back
// into "default" — compactness doesn't restrict capability.
func TestFocusConfig_ResolveWindow_SystemPromptDefaultsToCompact(t *testing.T) {
	f := FocusConfig{Enabled: true}
	for _, name := range []string{"chat", "files", "full", "heartbeat"} {
		profile, ok := f.ResolveWindow(name)
		if !ok {
			t.Fatalf("ResolveWindow(%s) ok = false", name)
		}
		if profile.SystemPromptMode != TurnProfileModeCompact {
			t.Fatalf("%s window SystemPromptMode = %q, want compact", name, profile.SystemPromptMode)
		}
	}
}

// TestFocusConfig_ResolveWindow_SystemPromptExplicitOverride covers the
// opt-out: a window can still ask for the full prompt explicitly.
func TestFocusConfig_ResolveWindow_SystemPromptExplicitOverride(t *testing.T) {
	f := FocusConfig{
		Enabled: true,
		Windows: map[string]FocusWindow{
			"chat": {SystemPrompt: TurnProfileModeDefault},
		},
	}
	profile, ok := f.ResolveWindow("chat")
	if !ok || profile.SystemPromptMode != TurnProfileModeDefault {
		t.Fatalf("profile = %+v, want explicit SystemPromptMode=default honored", profile)
	}
}

func TestFocusConfig_ResolveWindow_HeartbeatAndCronHaveHistoryOff(t *testing.T) {
	f := FocusConfig{Enabled: true}
	for _, name := range []string{"heartbeat", "cron"} {
		profile, ok := f.ResolveWindow(name)
		if !ok {
			t.Fatalf("ResolveWindow(%s) ok = false", name)
		}
		if profile.HistoryMode != TurnProfileModeOff {
			t.Fatalf("%s window HistoryMode = %q, want off", name, profile.HistoryMode)
		}
		if !profile.NeedsTime {
			t.Fatalf("%s window NeedsTime = false, want true", name)
		}
		if profile.EscalateTo != "" {
			t.Fatalf("%s window EscalateTo = %q, want empty", name, profile.EscalateTo)
		}
	}
}

func TestFocusConfig_MergedWindows_ConfigOverridesReplaceBuiltins(t *testing.T) {
	f := FocusConfig{
		Enabled: true,
		Windows: map[string]FocusWindow{
			"chat": {
				Tools: TurnProfileBlock{Mode: TurnProfileModeCustom, Allow: []string{"exec"}},
			},
			"custom-window": {
				Tools: TurnProfileBlock{Mode: TurnProfileModeOff},
			},
		},
	}
	merged := f.mergedWindows()
	if len(merged) != len(DefaultFocusWindows())+1 {
		t.Fatalf("merged window count = %d, want builtins+1", len(merged))
	}
	chat := merged["chat"]
	if chat.Tools.Mode != TurnProfileModeCustom || len(chat.Tools.Allow) != 1 || chat.Tools.Allow[0] != "exec" {
		t.Fatalf("overridden chat window = %+v, want custom exec-only tools", chat)
	}
	if _, ok := merged["custom-window"]; !ok {
		t.Fatal("custom-window not present in merged windows")
	}
}

func TestFocusConfig_EffectiveStickyTurns(t *testing.T) {
	f := FocusConfig{Enabled: true, StickyTurns: 5}
	if got := f.EffectiveStickyTurns("files"); got != 3 {
		t.Fatalf("files sticky turns = %d, want window override 3", got)
	}
	if got := f.EffectiveStickyTurns("unknown"); got != 0 {
		t.Fatalf("unknown window sticky turns = %d, want 0", got)
	}

	f2 := FocusConfig{
		Enabled:     true,
		StickyTurns: 7,
		Windows: map[string]FocusWindow{
			"custom": {},
		},
	}
	if got := f2.EffectiveStickyTurns("custom"); got != 7 {
		t.Fatalf("custom window (no override) sticky turns = %d, want config default 7", got)
	}
}

func TestFocusConfig_Escalation_Defaults(t *testing.T) {
	f := FocusConfig{}
	if !f.EscalationEnabled() {
		t.Fatal("EscalationEnabled() = false, want true by default (nil Enabled)")
	}
	if got := f.MaxEscalationsPerTurn(); got != 1 {
		t.Fatalf("MaxEscalationsPerTurn() = %d, want 1", got)
	}

	disabled := false
	f2 := FocusConfig{Escalation: FocusEscalationConfig{Enabled: &disabled, MaxPerTurn: 3}}
	if f2.EscalationEnabled() {
		t.Fatal("EscalationEnabled() = true, want false when explicitly disabled")
	}
	if got := f2.MaxEscalationsPerTurn(); got != 3 {
		t.Fatalf("MaxEscalationsPerTurn() = %d, want 3", got)
	}
}

func TestFocusConfig_ListWindowNames_SortedAndIncludesBuiltins(t *testing.T) {
	f := FocusConfig{Enabled: true}
	names := f.ListWindowNames()
	if len(names) != len(DefaultFocusWindows()) {
		t.Fatalf("ListWindowNames() len = %d, want %d", len(names), len(DefaultFocusWindows()))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("ListWindowNames() not sorted: %v", names)
		}
	}
}

func TestValidateFocus_DefaultMustBeConfiguredWindow(t *testing.T) {
	cfg := &Config{}
	cfg.Agents.Defaults.Focus = FocusConfig{Enabled: true, Default: "does-not-exist"}
	err := cfg.ValidateFocus()
	if err == nil || !strings.Contains(err.Error(), "focus.default") {
		t.Fatalf("ValidateFocus() error = %v, want focus.default complaint", err)
	}
}

func TestValidateFocus_OriginsMustReferenceKnownWindow(t *testing.T) {
	cfg := &Config{}
	cfg.Agents.Defaults.Focus = FocusConfig{
		Enabled: true,
		Origins: map[string]string{"cron": "ghost-window"},
	}
	err := cfg.ValidateFocus()
	if err == nil || !strings.Contains(err.Error(), "focus.origins") {
		t.Fatalf("ValidateFocus() error = %v, want focus.origins complaint", err)
	}
}

func TestValidateFocus_EscalateToMustReferenceKnownWindow(t *testing.T) {
	cfg := &Config{}
	bogus := "ghost-window"
	cfg.Agents.Defaults.Focus = FocusConfig{
		Enabled: true,
		Windows: map[string]FocusWindow{
			"chat": {EscalateTo: &bogus},
		},
	}
	err := cfg.ValidateFocus()
	if err == nil || !strings.Contains(err.Error(), "escalate_to") {
		t.Fatalf("ValidateFocus() error = %v, want escalate_to complaint", err)
	}
}

// TestValidateFocus_ExplicitCompactSystemPromptAccepted is a regression
// test: a window explicitly writing "system_prompt": "compact" (redundant
// with the default, but should still be legal) must validate — the
// validateTurnProfileBlock compact carve-out is keyed by field name, and
// focus.go's call site uses a longer "focus.windows.<name>.system_prompt"
// field name than the plain "system_prompt" the bare turn_profile call
// site uses.
func TestValidateFocus_ExplicitCompactSystemPromptAccepted(t *testing.T) {
	cfg := &Config{}
	cfg.Agents.Defaults.Focus = FocusConfig{
		Enabled: true,
		Windows: map[string]FocusWindow{
			"chat": {SystemPrompt: TurnProfileModeCompact},
		},
	}
	if err := cfg.ValidateFocus(); err != nil {
		t.Fatalf("ValidateFocus() error = %v, want nil for an explicit system_prompt=compact window", err)
	}
}

func TestValidateFocus_InvalidRegexRejected(t *testing.T) {
	cfg := &Config{}
	cfg.Agents.Defaults.Focus = FocusConfig{
		Enabled: true,
		Windows: map[string]FocusWindow{
			"chat": {Triggers: FocusTriggers{Regex: []string{"("}}},
		},
	}
	if err := cfg.ValidateFocus(); err == nil {
		t.Fatal("ValidateFocus() error = nil, want invalid regex to be rejected")
	}
}

func TestValidateFocus_UnsupportedToolsModeRejected(t *testing.T) {
	cfg := &Config{}
	cfg.Agents.Defaults.Focus = FocusConfig{
		Enabled: true,
		Windows: map[string]FocusWindow{
			"chat": {Tools: TurnProfileBlock{Mode: TurnProfileMode("bogus")}},
		},
	}
	if err := cfg.ValidateFocus(); err == nil {
		t.Fatal("ValidateFocus() error = nil, want unsupported tools mode rejected")
	}
}

func TestFocusConfig_JSONRoundTrip(t *testing.T) {
	raw := `{
		"enabled": true,
		"default": "chat",
		"sticky_turns": 2,
		"origins": {"heartbeat": "heartbeat", "cron": "cron"},
		"windows": {
			"files": {
				"tools": {"mode": "custom", "allow": ["read_file"]},
				"memory": "core",
				"sticky_turns": 4,
				"escalate_to": "full",
				"needs_time": false,
				"triggers": {"keywords": ["arquivo"], "regex": ["\\.md$"]}
			}
		},
		"escalation": {"enabled": true, "max_per_turn": 1}
	}`

	var f FocusConfig
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !f.Enabled || f.Default != "chat" || f.StickyTurns != 2 {
		t.Fatalf("parsed top-level fields = %+v", f)
	}
	files, ok := f.Windows["files"]
	if !ok {
		t.Fatal("parsed windows missing files")
	}
	if files.Tools.Mode != TurnProfileModeCustom || len(files.Tools.Allow) != 1 {
		t.Fatalf("parsed files.tools = %+v", files.Tools)
	}
	if files.effectiveStickyTurns(0) != 4 {
		t.Fatalf("parsed files sticky turns = %d, want 4", files.effectiveStickyTurns(0))
	}
	if files.effectiveEscalateTo() != "full" {
		t.Fatalf("parsed files escalate_to = %q, want full", files.effectiveEscalateTo())
	}

	cfg := &Config{}
	cfg.Agents.Defaults.Focus = f
	if err := cfg.ValidateFocus(); err != nil {
		t.Fatalf("ValidateFocus() error = %v on round-tripped config", err)
	}
}

func TestValidateReflexes_EmptyIsNoop(t *testing.T) {
	cfg := &Config{}
	if err := cfg.ValidateReflexes(); err != nil {
		t.Fatalf("ValidateReflexes() error = %v, want nil for empty list", err)
	}
}

func TestValidateReflexes_RejectsBadRegexAndAction(t *testing.T) {
	cfg := &Config{}
	cfg.Agents.Defaults.Reflexes = []ReflexConfig{
		{Match: "(", Action: ReflexActionReply, Template: "hi"},
	}
	if err := cfg.ValidateReflexes(); err == nil {
		t.Fatal("ValidateReflexes() error = nil, want invalid regex rejected")
	}

	cfg.Agents.Defaults.Reflexes = []ReflexConfig{
		{Match: "^hi$", Action: ReflexAction("teleport"), Template: "hi"},
	}
	if err := cfg.ValidateReflexes(); err == nil {
		t.Fatal("ValidateReflexes() error = nil, want unsupported action rejected")
	}

	cfg.Agents.Defaults.Reflexes = []ReflexConfig{
		{Match: "^hi$", Action: ReflexActionReply},
	}
	if err := cfg.ValidateReflexes(); err == nil {
		t.Fatal("ValidateReflexes() error = nil, want missing template rejected")
	}
}

func TestValidateReflexes_ValidReflexPasses(t *testing.T) {
	cfg := &Config{}
	cfg.Agents.Defaults.Reflexes = []ReflexConfig{
		{Match: "(?i)^status$", Action: ReflexActionCommand, Template: "/context"},
		{Match: "(?i)^oi$", Action: ReflexActionReply, Template: "Oi, {{sender}}!"},
		{Match: "(?i)^uptime$", Action: ReflexActionExec, Template: "uptime"},
	}
	if err := cfg.ValidateReflexes(); err != nil {
		t.Fatalf("ValidateReflexes() error = %v, want nil", err)
	}
}

func TestReflexConfig_EffectivePersistDefaults(t *testing.T) {
	reply := ReflexConfig{Action: ReflexActionReply}
	if !reply.EffectivePersist() {
		t.Fatal("reply reflex default persist = false, want true")
	}
	exec := ReflexConfig{Action: ReflexActionExec}
	if exec.EffectivePersist() {
		t.Fatal("exec reflex default persist = true, want false")
	}
	explicit := true
	overridden := ReflexConfig{Action: ReflexActionExec, Persist: &explicit}
	if !overridden.EffectivePersist() {
		t.Fatal("exec reflex with Persist=true should honor the override")
	}
}
