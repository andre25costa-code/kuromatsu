package config

import "testing"

// FR-022 phase 2: "retrieved" is a valid focus memory mode (case-insensitive)
// and the existing values keep their meaning.
func TestFocusWindow_EffectiveMemoryModeAcceptsRetrieved(t *testing.T) {
	for in, want := range map[string]string{
		"retrieved":   FocusMemoryRetrieved,
		" Retrieved ": FocusMemoryRetrieved,
		"core":        FocusMemoryCore,
		"off":         FocusMemoryOff,
		"":            FocusMemoryDefault,
		"bogus":       FocusMemoryDefault,
	} {
		if got := (FocusWindow{Memory: in}).effectiveMemoryMode(); got != want {
			t.Errorf("effectiveMemoryMode(%q) = %q, want %q", in, got, want)
		}
	}
}
