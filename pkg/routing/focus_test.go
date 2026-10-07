package routing

import (
	"context"
	"testing"
)

func testFocusRouter() *FocusRouter {
	return NewFocusRouter(FocusRouterConfig{
		Default: "chat",
		Origins: map[string]string{
			"heartbeat": "heartbeat",
			"cron":      "cron",
		},
		Rules: []FocusRule{
			{
				Window:   "files",
				Keywords: []string{"arquivo", "pasta", "file", "folder"},
				Regexes:  []string{`(^|\s)(\.{0,2}/|~/)[\w./-]+`},
			},
			{
				Window:   "shell",
				Keywords: []string{"execute", "comando", "run", "command"},
			},
			{
				Window:  "web",
				Regexes: []string{`https?://`},
			},
		},
	})
}

func TestFocusRouter_ExplicitWindowWinsOverEverything(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{
		ExplicitWindow: "full",
		Message:        "[foco:files] leia o arquivo",
		Origin:         "heartbeat",
	})
	if decision.Window != "full" || !decision.Explicit || decision.Reason != "explicit" {
		t.Fatalf("decision = %+v, want explicit full", decision)
	}
}

func TestFocusRouter_InlineTagStripsTagFromMessage(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Message: "[foco:files] leia o arquivo X"})
	if decision.Window != "files" || decision.Reason != "inline_tag" || !decision.Explicit {
		t.Fatalf("decision = %+v, want inline_tag files", decision)
	}
	if decision.Message != "leia o arquivo X" {
		t.Fatalf("decision.Message = %q, want tag stripped", decision.Message)
	}
}

func TestFocusRouter_InlineTagFocusAliasAlsoWorks(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Message: "[focus:shell] rode isso"})
	if decision.Window != "shell" || decision.Reason != "inline_tag" {
		t.Fatalf("decision = %+v, want inline_tag shell via [focus:...]", decision)
	}
}

func TestFocusRouter_NonUserOriginResolvesViaOrigins(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{
		Origin:  "heartbeat",
		Message: "leia o arquivo", // would otherwise match the "files" keyword rule
	})
	if decision.Window != "heartbeat" || decision.Reason != "origin" {
		t.Fatalf("decision = %+v, want origin heartbeat (not keyword rules)", decision)
	}
}

func TestFocusRouter_UserOriginIgnoresOriginsMap(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Origin: "user", Message: "oi"})
	if decision.Window != "chat" || decision.Reason != "default" {
		t.Fatalf("decision = %+v, want default chat for origin=user", decision)
	}
}

func TestFocusRouter_UnknownNonUserOriginFallsThroughToRules(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Origin: "reflex", Message: "rode o comando"})
	if decision.Window != "shell" || decision.Reason != "rule" {
		t.Fatalf("decision = %+v, want shell rule (origin has no mapping)", decision)
	}
}

func TestFocusRouter_KeywordMatchFoldsAccentsAndCase(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Message: "Preciso ler um ARQUIVO agora"})
	if decision.Window != "files" || decision.Reason != "rule" {
		t.Fatalf("decision = %+v, want files rule via case-folded keyword", decision)
	}

	decision2 := r.Route(context.Background(), FocusInput{Message: "abra a PASTA de projetos"})
	if decision2.Window != "files" {
		t.Fatalf("decision2 = %+v, want files via folded keyword", decision2)
	}
}

func TestFocusRouter_RegexMatchIsCaseInsensitive(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Message: "veja HTTPS://EXAMPLE.COM"})
	if decision.Window != "web" || decision.Reason != "rule" {
		t.Fatalf("decision = %+v, want web rule via case-insensitive regex", decision)
	}
}

func TestFocusRouter_KeywordCheckedBeforeRegexWithinSameRule(t *testing.T) {
	// The "files" rule's keyword list matches "arquivo" but the message
	// also happens to look like a path; either signal should resolve to
	// "files" — this just pins down that a rule's own keyword and regex
	// don't fight each other.
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Message: "leia /etc/arquivo.txt"})
	if decision.Window != "files" {
		t.Fatalf("decision = %+v, want files", decision)
	}
}

func TestFocusRouter_RuleOrderIsRespected(t *testing.T) {
	r := NewFocusRouter(FocusRouterConfig{
		Default: "chat",
		Rules: []FocusRule{
			{Window: "first", Keywords: []string{"palavra"}},
			{Window: "second", Keywords: []string{"palavra"}},
		},
	})
	decision := r.Route(context.Background(), FocusInput{Message: "essa palavra"})
	if decision.Window != "first" {
		t.Fatalf("decision.Window = %q, want first rule to win", decision.Window)
	}
}

func TestFocusRouter_NoRuleMatchesFallsBackToDefault(t *testing.T) {
	r := testFocusRouter()
	decision := r.Route(context.Background(), FocusInput{Message: "bom dia"})
	if decision.Window != "chat" || decision.Reason != "default" {
		t.Fatalf("decision = %+v, want default chat", decision)
	}
}

type stubMicroRouter struct {
	window string
	ok     bool
	calls  int
}

func (s *stubMicroRouter) Route(_ context.Context, _ FocusInput) (string, bool) {
	s.calls++
	return s.window, s.ok
}

func TestFocusRouter_MicroRouterOnlyConsultedWhenRulesFail(t *testing.T) {
	r := testFocusRouter()
	micro := &stubMicroRouter{window: "shell", ok: true}
	r.MicroRouter = micro

	// A message that already matches a keyword rule must not reach the
	// micro-router at all.
	r.Route(context.Background(), FocusInput{Message: "abra o arquivo"})
	if micro.calls != 0 {
		t.Fatalf("micro-router called %d times for a rule match, want 0", micro.calls)
	}

	decision := r.Route(context.Background(), FocusInput{Message: "algo sem sinal nenhum"})
	if micro.calls != 1 {
		t.Fatalf("micro-router called %d times for a non-matching message, want 1", micro.calls)
	}
	if decision.Window != "shell" || decision.Reason != "micro_router" {
		t.Fatalf("decision = %+v, want micro_router shell", decision)
	}
}

func TestFocusRouter_MicroRouterDecliningFallsBackToSticky(t *testing.T) {
	r := testFocusRouter()
	r.MicroRouter = &stubMicroRouter{ok: false}
	r.Remember("sess-1", "files", 2)

	decision := r.Route(context.Background(), FocusInput{SessionKey: "sess-1", Message: "algo neutro"})
	if decision.Window != "files" || decision.Reason != "sticky" || !decision.Sticky {
		t.Fatalf("decision = %+v, want sticky files", decision)
	}
}

func TestFocusRouter_StickyExpiresAfterConfiguredTurns(t *testing.T) {
	r := testFocusRouter()
	r.Remember("sess-1", "files", 2)

	for i := 0; i < 2; i++ {
		decision := r.Route(context.Background(), FocusInput{SessionKey: "sess-1", Message: "oi"})
		if decision.Window != "files" || !decision.Sticky {
			t.Fatalf("turn %d: decision = %+v, want sticky files", i, decision)
		}
	}

	decision := r.Route(context.Background(), FocusInput{SessionKey: "sess-1", Message: "oi"})
	if decision.Window != "chat" || decision.Sticky {
		t.Fatalf("after expiry: decision = %+v, want default chat", decision)
	}
}

func TestFocusRouter_StickyIndefiniteWhenTurnsNegative(t *testing.T) {
	r := testFocusRouter()
	r.Remember("sess-1", "files", -1)

	for i := 0; i < 10; i++ {
		decision := r.Route(context.Background(), FocusInput{SessionKey: "sess-1", Message: "oi"})
		if decision.Window != "files" || !decision.Sticky {
			t.Fatalf("turn %d: decision = %+v, want indefinitely sticky files", i, decision)
		}
	}
}

func TestFocusRouter_ForgetClearsSticky(t *testing.T) {
	r := testFocusRouter()
	r.Remember("sess-1", "files", -1)
	r.Forget("sess-1")

	decision := r.Route(context.Background(), FocusInput{SessionKey: "sess-1", Message: "oi"})
	if decision.Window != "chat" || decision.Sticky {
		t.Fatalf("decision after Forget = %+v, want default chat", decision)
	}
	if _, ok := r.Current("sess-1"); ok {
		t.Fatal("Current() ok = true after Forget, want false")
	}
}

func TestFocusRouter_RememberZeroTurnsForgets(t *testing.T) {
	r := testFocusRouter()
	r.Remember("sess-1", "files", 3)
	r.Remember("sess-1", "files", 0)
	if _, ok := r.Current("sess-1"); ok {
		t.Fatal("Current() ok = true after Remember(turns=0), want false (forgotten)")
	}
}

func TestFocusRouter_CurrentDoesNotConsumeATurn(t *testing.T) {
	r := testFocusRouter()
	r.Remember("sess-1", "files", 1)

	for i := 0; i < 5; i++ {
		window, ok := r.Current("sess-1")
		if !ok || window != "files" {
			t.Fatalf("Current() = (%q, %v), want (files, true) on peek %d", window, ok, i)
		}
	}
	// A real Route call still only has the single remembered turn to spend.
	decision := r.Route(context.Background(), FocusInput{SessionKey: "sess-1", Message: "oi"})
	if decision.Window != "files" {
		t.Fatalf("decision = %+v, want files (first consuming Route call)", decision)
	}
	decision2 := r.Route(context.Background(), FocusInput{SessionKey: "sess-1", Message: "oi"})
	if decision2.Window != "chat" {
		t.Fatalf("decision2 = %+v, want default chat (sticky turn spent)", decision2)
	}
}

func TestFocusRouter_EmptySessionKeyNeverSticky(t *testing.T) {
	r := testFocusRouter()
	r.Remember("", "files", -1) // no-op: Remember treats empty sessionKey as Forget
	decision := r.Route(context.Background(), FocusInput{Message: "oi"})
	if decision.Window != "chat" {
		t.Fatalf("decision = %+v, want default chat", decision)
	}
}
