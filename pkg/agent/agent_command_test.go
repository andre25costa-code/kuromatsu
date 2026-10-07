// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"strings"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// newFocusCommandAgentLoop builds a minimal AgentLoop for exercising
// applyExplicitFocusCommand directly (FR-014 AC-014-8) — no LLM call is ever
// made, so the capture provider only needs to satisfy providers.LLMProvider.
func newFocusCommandAgentLoop(t *testing.T, cfg *config.Config) *AgentLoop {
	t.Helper()
	if cfg.Agents.Defaults.Workspace == "" {
		cfg.Agents.Defaults.Workspace = t.TempDir()
	}
	return NewAgentLoop(cfg, bus.NewMessageBus(), &turnProfileCaptureProvider{})
}

// TestApplyExplicitFocusCommand_NoArgsListsWindowsAndCurrent covers the
// "/foco" (no args) form: it lists every available window and reports the
// current one (auto, since nothing was pinned yet for this session).
func TestApplyExplicitFocusCommand_NoArgsListsWindowsAndCurrent(t *testing.T) {
	cfg := defaultFocusChatCfg()
	al := newFocusCommandAgentLoop(t, cfg)
	fr := al.focusRuntimeSnapshot()
	if fr == nil {
		t.Fatal("focusRuntimeSnapshot() = nil, want an enabled focus runtime")
	}

	opts := &processOptions{SessionKey: "agent:default:test-foco-list"}
	matched, handled, reply := al.applyExplicitFocusCommand("/foco", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want true/true for bare /foco", matched, handled)
	}
	if !strings.Contains(reply, "Current window: auto") {
		t.Fatalf("reply = %q, want it to report the auto/unpinned current window", reply)
	}
	for _, name := range fr.ListWindowNames() {
		if !strings.Contains(reply, name) {
			t.Fatalf("reply = %q, missing window %q from the available list", reply, name)
		}
	}

	// The "/focus" alias must behave identically.
	matched, handled, aliasReply := al.applyExplicitFocusCommand("/focus", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want true/true for the /focus alias", matched, handled)
	}
	if aliasReply != reply {
		t.Fatalf("/focus reply = %q, want it identical to /foco's %q", aliasReply, reply)
	}
}

// TestApplyExplicitFocusCommand_WindowOnlyPinsAderenciaIndefinitely covers
// "/foco <window>" with no message: it pins session aderência indefinitely
// (routing.FocusRouter.Remember with turns < 0) rather than falling through
// to the LLM.
func TestApplyExplicitFocusCommand_WindowOnlyPinsAderenciaIndefinitely(t *testing.T) {
	cfg := defaultFocusChatCfg()
	al := newFocusCommandAgentLoop(t, cfg)
	fr := al.focusRuntimeSnapshot()
	sessionKey := "agent:default:test-foco-pin"

	opts := &processOptions{SessionKey: sessionKey}
	matched, handled, reply := al.applyExplicitFocusCommand("/foco files", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want true/true for /foco files", matched, handled)
	}
	if !strings.Contains(reply, "files") || !strings.Contains(reply, "pinned") {
		t.Fatalf("reply = %q, want confirmation that files is pinned", reply)
	}

	window, sticky := fr.Current(sessionKey)
	if window != "files" || !sticky {
		t.Fatalf("fr.Current() = (%q, %v), want (files, true)", window, sticky)
	}

	// Peeking again (as a bare "/foco" would) must still report it pinned —
	// Current() never consumes a turn, unlike the router's own Route().
	window, sticky = fr.Current(sessionKey)
	if window != "files" || !sticky {
		t.Fatalf("fr.Current() on second peek = (%q, %v), want it to remain pinned indefinitely", window, sticky)
	}
}

// TestApplyExplicitFocusCommand_WindowAndMessageRewritesInlineTagWithoutPinning
// covers "/foco <window> <message>": it must rewrite just this message with
// the inline "[foco:<window>]" tag and fall through to the LLM (matched,
// !handled) — critically, it must NOT arm session aderência, unlike the
// window-only form above.
func TestApplyExplicitFocusCommand_WindowAndMessageRewritesInlineTagWithoutPinning(t *testing.T) {
	cfg := defaultFocusChatCfg()
	al := newFocusCommandAgentLoop(t, cfg)
	fr := al.focusRuntimeSnapshot()
	sessionKey := "agent:default:test-foco-inline"

	opts := &processOptions{SessionKey: sessionKey}
	matched, handled, reply := al.applyExplicitFocusCommand("/foco shell rode isso ai", opts)
	if !matched || handled {
		t.Fatalf("matched=%v handled=%v, want true/false (falls through to the LLM)", matched, handled)
	}
	if reply != "" {
		t.Fatalf("reply = %q, want empty (no immediate reply on the fall-through form)", reply)
	}
	const wantTagged = "[foco:shell] rode isso ai"
	if opts.Dispatch.UserMessage != wantTagged {
		t.Fatalf("opts.Dispatch.UserMessage = %q, want %q", opts.Dispatch.UserMessage, wantTagged)
	}
	if opts.UserMessage != wantTagged {
		t.Fatalf("opts.UserMessage = %q, want %q", opts.UserMessage, wantTagged)
	}

	if window, sticky := fr.Current(sessionKey); window != "" || sticky {
		t.Fatalf(
			"fr.Current() = (%q, %v), want no aderência armed by the <window> <message> form",
			window, sticky,
		)
	}
}

// TestApplyExplicitFocusCommand_AutoAndOffForgetPinnedWindow covers "/foco
// auto" and "/foco off": both must clear any pinned aderência and return to
// automatic routing.
func TestApplyExplicitFocusCommand_AutoAndOffForgetPinnedWindow(t *testing.T) {
	cfg := defaultFocusChatCfg()
	al := newFocusCommandAgentLoop(t, cfg)
	fr := al.focusRuntimeSnapshot()

	for _, forgetArg := range []string{"auto", "off"} {
		sessionKey := "agent:default:test-foco-forget-" + forgetArg
		opts := &processOptions{SessionKey: sessionKey}

		if matched, handled, _ := al.applyExplicitFocusCommand("/foco shell", opts); !matched || !handled {
			t.Fatalf("setup pin: matched=%v handled=%v, want true/true", matched, handled)
		}
		if window, sticky := fr.Current(sessionKey); window != "shell" || !sticky {
			t.Fatalf("setup pin: fr.Current() = (%q, %v), want (shell, true)", window, sticky)
		}

		matched, handled, reply := al.applyExplicitFocusCommand("/foco "+forgetArg, opts)
		if !matched || !handled {
			t.Fatalf("/foco %s: matched=%v handled=%v, want true/true", forgetArg, matched, handled)
		}
		if !strings.Contains(strings.ToLower(reply), "automatic") {
			t.Fatalf("/foco %s reply = %q, want it to mention automatic routing", forgetArg, reply)
		}
		if window, sticky := fr.Current(sessionKey); window != "" || sticky {
			t.Fatalf("/foco %s: fr.Current() = (%q, %v), want cleared (empty, false)", forgetArg, window, sticky)
		}
	}
}

// TestApplyExplicitFocusCommand_DisabledRepliesFocusDisabled covers
// AC-014-8's "focus.enabled=false" branch: the command still matches (so it
// never falls through to the LLM as a literal "/foco" message) but replies
// that focus windows are disabled instead of touching any router state.
func TestApplyExplicitFocusCommand_DisabledRepliesFocusDisabled(t *testing.T) {
	cfg := &config.Config{} // Focus.Enabled zero-value (false)
	al := newFocusCommandAgentLoop(t, cfg)
	if fr := al.focusRuntimeSnapshot(); fr != nil {
		t.Fatal("focusRuntimeSnapshot() != nil, want nil with focus.enabled=false")
	}

	opts := &processOptions{SessionKey: "agent:default:test-foco-disabled"}
	matched, handled, reply := al.applyExplicitFocusCommand("/foco", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want true/true even when focus is disabled", matched, handled)
	}
	if !strings.Contains(strings.ToLower(reply), "disabled") {
		t.Fatalf("reply = %q, want it to say focus windows are disabled", reply)
	}
}

// TestApplyExplicitFocusCommand_UnknownWindowListsAvailable covers the
// input-validation path: an unresolvable window name is rejected up front
// (never pinned, never routed) and the reply lists the real window names.
func TestApplyExplicitFocusCommand_UnknownWindowListsAvailable(t *testing.T) {
	cfg := defaultFocusChatCfg()
	al := newFocusCommandAgentLoop(t, cfg)
	fr := al.focusRuntimeSnapshot()
	sessionKey := "agent:default:test-foco-unknown-window"

	opts := &processOptions{SessionKey: sessionKey}
	matched, handled, reply := al.applyExplicitFocusCommand("/foco not-a-real-window", opts)
	if !matched || !handled {
		t.Fatalf("matched=%v handled=%v, want true/true for an unknown window name", matched, handled)
	}
	if !strings.Contains(reply, "Unknown focus window") {
		t.Fatalf("reply = %q, want it to reject the unknown window", reply)
	}
	for _, name := range fr.ListWindowNames() {
		if !strings.Contains(reply, name) {
			t.Fatalf("reply = %q, missing window %q from the available list", reply, name)
		}
	}
	if window, sticky := fr.Current(sessionKey); window != "" || sticky {
		t.Fatalf("fr.Current() = (%q, %v), want nothing pinned after an unknown window", window, sticky)
	}
}

// TestApplyExplicitFocusCommand_NonFocusCommandDoesNotMatch is the
// AC-014-9-adjacent guard: an unrelated command must not be intercepted by
// the /foco special case at all.
func TestApplyExplicitFocusCommand_NonFocusCommandDoesNotMatch(t *testing.T) {
	cfg := defaultFocusChatCfg()
	al := newFocusCommandAgentLoop(t, cfg)

	opts := &processOptions{SessionKey: "agent:default:test-foco-not-a-match"}
	matched, handled, reply := al.applyExplicitFocusCommand("/use shell", opts)
	if matched || handled || reply != "" {
		t.Fatalf("matched=%v handled=%v reply=%q, want false/false/\"\" for an unrelated command", matched, handled, reply)
	}
}
