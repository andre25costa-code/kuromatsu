package commands

import (
	"context"
	"errors"
	"testing"
)

func TestParseStatsArgs(t *testing.T) {
	cases := []struct {
		text       string
		wantWindow string
		wantHours  int
	}{
		{"/stats", "", 0},
		{"/stats 24", "", 24},
		{"/stats files", "files", 0},
		{"/stats files 24", "files", 24},
		{"/stats 24 files", "files", 24}, // order-independent
	}
	for _, tc := range cases {
		window, hours := parseStatsArgs(tc.text)
		if window != tc.wantWindow || hours != tc.wantHours {
			t.Errorf("parseStatsArgs(%q) = (%q, %d), want (%q, %d)",
				tc.text, window, hours, tc.wantWindow, tc.wantHours)
		}
	}
}

func TestStatsCommand_NilQueryStatsIsUnavailable(t *testing.T) {
	def := statsCommand()
	var reply string
	err := def.Handler(context.Background(), Request{
		Text: "/stats",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	}, &Runtime{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if reply != unavailableMsg {
		t.Fatalf("reply = %q, want %q", reply, unavailableMsg)
	}
}

func TestStatsCommand_NilRuntimeIsUnavailable(t *testing.T) {
	def := statsCommand()
	var reply string
	err := def.Handler(context.Background(), Request{
		Text: "/stats",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if reply != unavailableMsg {
		t.Fatalf("reply = %q, want %q", reply, unavailableMsg)
	}
}

func TestStatsCommand_PropagatesQueryStatsErrorAsReply(t *testing.T) {
	def := statsCommand()
	rt := &Runtime{
		QueryStats: func(_ context.Context, window string, hours int) (string, error) {
			return "", errors.New("telemetry is disabled (telemetry.enabled=false)")
		},
	}
	var reply string
	err := def.Handler(context.Background(), Request{
		Text: "/stats",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	}, rt)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if reply != "telemetry is disabled (telemetry.enabled=false)" {
		t.Fatalf("reply = %q, want the error message surfaced directly", reply)
	}
}

func TestStatsCommand_PassesParsedArgsAndRepliesWithReport(t *testing.T) {
	def := statsCommand()
	var gotWindow string
	var gotHours int
	rt := &Runtime{
		QueryStats: func(_ context.Context, window string, hours int) (string, error) {
			gotWindow, gotHours = window, hours
			return "report text", nil
		},
	}
	var reply string
	err := def.Handler(context.Background(), Request{
		Text: "/stats files 24",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	}, rt)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if gotWindow != "files" || gotHours != 24 {
		t.Fatalf("QueryStats called with (%q, %d), want (%q, %d)", gotWindow, gotHours, "files", 24)
	}
	if reply != "report text" {
		t.Fatalf("reply = %q, want %q", reply, "report text")
	}
}
