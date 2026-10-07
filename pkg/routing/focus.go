// PicoClaw - Ultra-lightweight personal AI agent

package routing

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// FocusRule is one config-ordered keyword/regex rule mapped to a focus
// window (ADR-014 point 1). Keywords are matched as case/accent-folded
// substrings; regexes are matched case-insensitively against the raw
// message. Within a rule, keywords are checked before regexes; rules are
// checked in the order given.
type FocusRule struct {
	Window   string
	Keywords []string
	Regexes  []string
}

// FocusRouterConfig is the plain-struct mirror of config.FocusConfig this
// package consumes — pkg/routing never imports pkg/config, matching the
// existing RouterConfig/config.RoutingConfig split in router.go. The agent
// layer (pkg/agent/focus.go) is responsible for translating one into the
// other.
type FocusRouterConfig struct {
	// Default is returned when nothing else matches.
	Default string
	// Origins maps a non-user turn origin ("heartbeat", "cron", "system",
	// "reflex") to the window it always resolves to.
	Origins map[string]string
	// Rules are evaluated in order after origin resolution.
	Rules []FocusRule
}

// FocusMicroRouter is an optional hook for a future LLM-backed
// micro-router (ADR-014 "Alternativas"; not implemented — nil by default).
// It is consulted only after every keyword/regex rule has failed to match,
// and only for origin "user" (or unset origin) messages.
type FocusMicroRouter interface {
	Route(ctx context.Context, input FocusInput) (window string, ok bool)
}

// FocusInput is one routing request.
type FocusInput struct {
	// Origin is "user" (the zero value is treated as "user"), "heartbeat",
	// "cron", "system", or "reflex". Only "user" (and the zero value) goes
	// through the inline-tag/keyword/regex/sticky pipeline; every other
	// origin resolves directly via FocusRouterConfig.Origins.
	Origin string
	// SessionKey identifies the session for sticky-window bookkeeping
	// (Remember/Forget/Current). Empty disables stickiness for this call.
	SessionKey string
	// Message is the raw user message, inline focus tag included.
	Message string
	// ExplicitWindow, when non-empty, short-circuits every other rule
	// (used by the /foco command's "route just this message" form).
	ExplicitWindow string
}

// FocusDecision is the router's output.
type FocusDecision struct {
	Window string
	// Reason explains which precedence step produced Window: "explicit",
	// "inline_tag", "origin", "rule", "micro_router", "sticky", or
	// "default".
	Reason string
	// Explicit is true for "explicit" and "inline_tag" — the caller asked
	// for this window by name, not a routing guess.
	Explicit bool
	// Sticky is true when Window came from session aderência rather than
	// a fresh match this turn.
	Sticky bool
	// Message is the input message with any leading inline focus tag
	// stripped (identical to the input otherwise). Callers should persist
	// this value, not the original, so the tag never lands in history.
	Message string
}

type focusStickyEntry struct {
	window string
	// remaining counts down on every FocusRouter.Route call that consults
	// it; <0 means "pinned indefinitely" (used by an explicit /foco
	// <window> command with no message) and never decrements.
	remaining int
}

type compiledFocusRule struct {
	window   string
	keywords []string
	regexes  []*regexp.Regexp
}

// FocusRouter resolves a focus window per message, deterministically and
// at zero LLM cost (ADR-014 point 1 / FR-014). It is safe for concurrent
// use.
type FocusRouter struct {
	cfg FocusRouterConfig
	// MicroRouter is exported so callers can wire a future implementation
	// after construction; nil (the default) disables the step entirely.
	MicroRouter FocusMicroRouter

	compiled []compiledFocusRule
	sticky   sync.Map // sessionKey -> focusStickyEntry
}

// NewFocusRouter compiles cfg's keyword/regex rules once up front.
func NewFocusRouter(cfg FocusRouterConfig) *FocusRouter {
	r := &FocusRouter{cfg: cfg}
	r.compiled = make([]compiledFocusRule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		cr := compiledFocusRule{window: strings.TrimSpace(rule.Window)}
		if cr.window == "" {
			continue
		}
		for _, kw := range rule.Keywords {
			if folded := foldFocusText(kw); folded != "" {
				cr.keywords = append(cr.keywords, folded)
			}
		}
		for _, pattern := range rule.Regexes {
			p := pattern
			if !strings.HasPrefix(p, "(?i)") {
				p = "(?i)" + p
			}
			if re, err := regexp.Compile(p); err == nil {
				cr.regexes = append(cr.regexes, re)
			}
		}
		r.compiled = append(r.compiled, cr)
	}
	return r
}

var inlineFocusTagRe = regexp.MustCompile(`(?i)^\s*\[(?:foco|focus):\s*([a-zA-Z0-9_-]+)\s*\]\s*`)

// extractInlineFocusTag looks for a leading "[foco:<window>]" (or
// "[focus:<window>]") tag and returns the window name and the message with
// the tag removed.
func extractInlineFocusTag(message string) (window, stripped string, ok bool) {
	loc := inlineFocusTagRe.FindStringSubmatchIndex(message)
	if loc == nil {
		return "", message, false
	}
	return message[loc[2]:loc[3]], message[loc[1]:], true
}

// Route decides a window for input, following the precedence order from
// ADR-014 point 1: explicit -> inline tag -> non-user origin -> configured
// rules (keyword, then regex, in rule order) -> optional MicroRouter ->
// session aderência -> Default.
func (r *FocusRouter) Route(ctx context.Context, input FocusInput) FocusDecision {
	message := input.Message

	if explicit := strings.TrimSpace(input.ExplicitWindow); explicit != "" {
		return FocusDecision{Window: explicit, Reason: "explicit", Explicit: true, Message: message}
	}

	if window, stripped, ok := extractInlineFocusTag(message); ok {
		return FocusDecision{Window: window, Reason: "inline_tag", Explicit: true, Message: stripped}
	}

	origin := strings.ToLower(strings.TrimSpace(input.Origin))
	if origin != "" && origin != "user" {
		if window, ok := r.cfg.Origins[origin]; ok && strings.TrimSpace(window) != "" {
			return FocusDecision{Window: window, Reason: "origin", Message: message}
		}
	}

	folded := foldFocusText(message)
	for _, rule := range r.compiled {
		for _, kw := range rule.keywords {
			if kw != "" && strings.Contains(folded, kw) {
				return FocusDecision{Window: rule.window, Reason: "rule", Message: message}
			}
		}
		for _, re := range rule.regexes {
			if re.MatchString(message) {
				return FocusDecision{Window: rule.window, Reason: "rule", Message: message}
			}
		}
	}

	if r != nil && r.MicroRouter != nil {
		if window, ok := r.MicroRouter.Route(ctx, input); ok && strings.TrimSpace(window) != "" {
			return FocusDecision{Window: strings.TrimSpace(window), Reason: "micro_router", Message: message}
		}
	}

	if window, ok := r.currentSticky(input.SessionKey); ok {
		return FocusDecision{Window: window, Reason: "sticky", Sticky: true, Message: message}
	}

	return FocusDecision{Window: r.cfg.Default, Reason: "default", Message: message}
}

// currentSticky consults (and, for a finite entry, decrements) the
// remembered window for sessionKey.
func (r *FocusRouter) currentSticky(sessionKey string) (string, bool) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", false
	}
	val, ok := r.sticky.Load(sessionKey)
	if !ok {
		return "", false
	}
	entry := val.(focusStickyEntry)
	if entry.remaining == 0 {
		r.sticky.Delete(sessionKey)
		return "", false
	}
	if entry.remaining > 0 {
		entry.remaining--
		if entry.remaining <= 0 {
			r.sticky.Delete(sessionKey)
		} else {
			r.sticky.Store(sessionKey, entry)
		}
	}
	return entry.window, true
}

// Current peeks at the remembered window for sessionKey without consuming
// a turn (used by "/foco" with no arguments to report the active window).
func (r *FocusRouter) Current(sessionKey string) (string, bool) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", false
	}
	val, ok := r.sticky.Load(sessionKey)
	if !ok {
		return "", false
	}
	entry := val.(focusStickyEntry)
	if entry.remaining == 0 {
		return "", false
	}
	return entry.window, true
}

// Remember pins window for sessionKey. turns > 0 means "for turns more
// Route calls that don't find a stronger match"; turns < 0 means "pinned
// indefinitely, until Forget or another Remember"; turns == 0 (or an empty
// sessionKey/window) forgets any existing pin instead.
func (r *FocusRouter) Remember(sessionKey, window string, turns int) {
	sessionKey = strings.TrimSpace(sessionKey)
	window = strings.TrimSpace(window)
	if sessionKey == "" || window == "" || turns == 0 {
		r.Forget(sessionKey)
		return
	}
	r.sticky.Store(sessionKey, focusStickyEntry{window: window, remaining: turns})
}

// Forget clears any remembered window for sessionKey.
func (r *FocusRouter) Forget(sessionKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	r.sticky.Delete(sessionKey)
}

// focusAccentFolder strips the PT-BR/EN Latin-1 accented characters that
// realistically show up in trigger keywords or user messages, so
// "memória"/"memoria" and "não"/"nao" match the same folded keyword without
// pulling in a Unicode normalization dependency.
var focusAccentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

func foldFocusText(s string) string {
	return focusAccentFolder.Replace(strings.ToLower(s))
}
