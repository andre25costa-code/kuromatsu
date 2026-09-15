// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/routing"
)

// Focus origins (ADR-014/S16, processOptions.Origin / routing.FocusInput.Origin).
// "" is always treated the same as OriginUser.
const (
	OriginUser      = "user"
	OriginHeartbeat = "heartbeat"
	OriginCron      = "cron"
	OriginSystem    = "system"
	OriginReflex    = "reflex"
)

// focusRuntime bridges config.FocusConfig to routing.FocusRouter for one
// AgentLoop generation. A nil *focusRuntime means "focus disabled" —
// applyFocus below checks for it first and is a complete no-op otherwise,
// which is what keeps focus.enabled=false byte-identical to today
// (AC-014-9). It is recreated (never mutated in place) by NewAgentLoop and
// ReloadProviderAndConfig, so aderência/sticky state resets alongside
// everything else a config reload already resets — the same lifecycle the
// steering queue and hook manager already have.
type focusRuntime struct {
	cfg    config.FocusConfig
	router *routing.FocusRouter
}

// newFocusRuntime builds a focusRuntime from cfg, or nil when focus is
// disabled (or cfg is nil).
func newFocusRuntime(cfg *config.Config) *focusRuntime {
	if cfg == nil {
		return nil
	}
	fc := cfg.Agents.Defaults.Focus
	if !fc.Enabled {
		return nil
	}
	return &focusRuntime{
		cfg:    fc,
		router: routing.NewFocusRouter(focusRouterConfigFromFocusConfig(fc)),
	}
}

// focusRouterConfigFromFocusConfig translates config.FocusConfig into the
// plain routing.FocusRouterConfig pkg/routing consumes (pkg/routing never
// imports pkg/config — see routing/focus.go). Window iteration order is
// alphabetical (FocusConfig.ListWindowNames) for determinism; windows with
// no triggers are skipped since they can never match a rule anyway.
func focusRouterConfigFromFocusConfig(fc config.FocusConfig) routing.FocusRouterConfig {
	def := strings.ToLower(strings.TrimSpace(fc.Default))
	if def == "" {
		def = config.DefaultFocusWindow
	}

	names := fc.ListWindowNames()
	windows := fc.MergedWindows()
	rules := make([]routing.FocusRule, 0, len(names))
	for _, name := range names {
		w := windows[name]
		if len(w.Triggers.Keywords) == 0 && len(w.Triggers.Regex) == 0 {
			continue
		}
		rules = append(rules, routing.FocusRule{
			Window:   name,
			Keywords: append([]string(nil), w.Triggers.Keywords...),
			Regexes:  append([]string(nil), w.Triggers.Regex...),
		})
	}

	// MergedOrigins (not fc.Origins directly) so "heartbeat"/"cron" resolve
	// to their eponymous built-in windows out of the box (ADR-014 point 7 /
	// S16 step 7 / AC-014-3) without requiring focus.origins in config —
	// config.FocusConfig.Origins only overrides or (via an explicit ""
	// value) disables a built-in mapping, matching how Windows/
	// DefaultFocusWindows already work.
	origins := fc.MergedOrigins()

	return routing.FocusRouterConfig{Default: def, Origins: origins, Rules: rules}
}

// ListWindowNames returns the sorted focus window names, or nil when focus
// is disabled — used by the /foco command.
func (fr *focusRuntime) ListWindowNames() []string {
	if fr == nil {
		return nil
	}
	return fr.cfg.ListWindowNames()
}

// Current returns the sticky window remembered for sessionKey, if any.
func (fr *focusRuntime) Current(sessionKey string) (string, bool) {
	if fr == nil || fr.router == nil {
		return "", false
	}
	return fr.router.Current(sessionKey)
}

// Forget clears any remembered window for sessionKey (the "/foco auto" and
// "/foco off" forms).
func (fr *focusRuntime) Forget(sessionKey string) {
	if fr == nil || fr.router == nil {
		return
	}
	fr.router.Forget(sessionKey)
}

// Pin arms indefinite aderência for sessionKey — the "/foco <window>" form
// with no message.
func (fr *focusRuntime) Pin(sessionKey, window string) {
	if fr == nil || fr.router == nil {
		return
	}
	fr.router.Remember(sessionKey, window, -1)
}

// ResolveWindow exposes FocusConfig.ResolveWindow for callers (the /foco
// command) that need to validate a window name without going through the
// router.
func (fr *focusRuntime) ResolveWindow(name string) (config.EffectiveTurnProfile, bool) {
	if fr == nil {
		return config.EffectiveTurnProfile{}, false
	}
	return fr.cfg.ResolveWindow(name)
}

// focusRuntimeSnapshot reads al.focus under the same lock cfg/registry use,
// so a config reload swapping it out mid-turn never races.
func (al *AgentLoop) focusRuntimeSnapshot() *focusRuntime {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.focus
}

// deriveFocusOrigin resolves the effective focus origin for a turn: an
// explicit opts.Origin wins; otherwise it's inferred the way ADR-014's
// Trilho A describes — SessionKey "heartbeat", SenderID "cron", Channel
// "system" — falling back to OriginUser.
func deriveFocusOrigin(opts processOptions) string {
	if origin := strings.ToLower(strings.TrimSpace(opts.Origin)); origin != "" {
		return origin
	}
	if strings.TrimSpace(opts.Dispatch.SessionKey) == OriginHeartbeat {
		return OriginHeartbeat
	}
	if opts.Dispatch.SenderID() == OriginCron {
		return OriginCron
	}
	if opts.Dispatch.Channel() == OriginSystem {
		return OriginSystem
	}
	return OriginUser
}

// applyFocus is the FR-014/ADR-014 router entry point, called from
// runAgentLoop right after resolveTurnProfileOptions — the single funnel
// every entry path (user message, /btw, heartbeat, cron, system) already
// goes through. With focus.enabled=false (al.focus == nil) it returns opts
// completely untouched: no field is read or written, which is exactly what
// keeps that path byte-identical to today (AC-014-9).
func (al *AgentLoop) applyFocus(opts processOptions) processOptions {
	fr := al.focusRuntimeSnapshot()
	if fr == nil {
		return opts
	}

	origin := deriveFocusOrigin(opts)
	message := opts.Dispatch.UserMessage

	decision := fr.router.Route(context.Background(), routing.FocusInput{
		Origin:     origin,
		SessionKey: opts.Dispatch.SessionKey,
		Message:    message,
	})

	profile, ok := fr.ResolveWindow(decision.Window)
	if !ok {
		// A misconfigured/unknown window name shouldn't happen once
		// ValidateFocus has run at boot, but never silently break a turn
		// over it — fall through with everything else untouched.
		logger.WarnCF("agent", "Focus router selected an unresolvable window; ignoring", map[string]any{
			"window": decision.Window,
			"reason": decision.Reason,
			"origin": origin,
		})
		return opts
	}

	opts.TurnProfile = profile
	opts.FocusWindow = profile.Window
	opts.Origin = origin
	// The resolved window is authoritative over history — this can
	// override a NoHistory that resolveTurnProfileOptions set moments ago
	// from the plain, static turn_profile (or that a caller like
	// ProcessHeartbeat set directly), which is the correct behavior once
	// focus is actively deciding the window.
	if profile.HistoryMode == config.TurnProfileModeOff {
		opts.NoHistory = true
		opts.EnableSummary = false
	} else {
		opts.NoHistory = false
	}
	if decision.Message != message {
		// Strip the inline [foco:window] tag before it reaches the
		// prompt/session — FR-014 AC-014-2.
		opts.Dispatch.UserMessage = decision.Message
		opts.UserMessage = decision.Message
	}

	logger.InfoCF("agent", "Focus window selected", map[string]any{
		"window":      profile.Window,
		"reason":      decision.Reason,
		"explicit":    decision.Explicit,
		"sticky":      decision.Sticky,
		"origin":      origin,
		"session_key": opts.Dispatch.SessionKey,
	})

	return opts
}

// rememberFocusWindow arms/refreshes session aderência for window after a
// successfully completed turn — only for user-origin turns (ADR-014 point
// 1); heartbeat/cron/system never build up sticky state, matching FR-014
// AC-014-3 (their window always comes from Origins, never from stickiness).
func (al *AgentLoop) rememberFocusWindow(origin, sessionKey, window string) {
	fr := al.focusRuntimeSnapshot()
	if fr == nil || fr.router == nil || window == "" || origin != OriginUser {
		return
	}
	turns := fr.cfg.EffectiveStickyTurns(window)
	if turns <= 0 {
		fr.router.Forget(sessionKey)
		return
	}
	fr.router.Remember(sessionKey, window, turns)
}
