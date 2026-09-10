package sleep

import (
	"testing"
	"time"
)

func mustParseWindow(t *testing.T, s string) Window {
	t.Helper()
	w, err := ParseWindow(s)
	if err != nil {
		t.Fatalf("ParseWindow(%q) error = %v", s, err)
	}
	return w
}

func TestParseWindow_Valid(t *testing.T) {
	w := mustParseWindow(t, "03:00-05:30")
	want := Window{StartHour: 3, StartMinute: 0, EndHour: 5, EndMinute: 30}
	if w != want {
		t.Fatalf("got %+v, want %+v", w, want)
	}
}

func TestParseWindow_Invalid(t *testing.T) {
	cases := []string{"", "03:00", "25:00-05:00", "03:00-05:99", "aa:bb-05:00"}
	for _, c := range cases {
		if _, err := ParseWindow(c); err == nil {
			t.Fatalf("ParseWindow(%q): expected an error", c)
		}
	}
}

func TestWindow_NextStart_LaterToday(t *testing.T) {
	w := mustParseWindow(t, "03:00-05:00")
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	got := w.NextStart(now)
	want := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWindow_NextStart_RollsToTomorrow(t *testing.T) {
	w := mustParseWindow(t, "03:00-05:00")
	now := time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC)
	got := w.NextStart(now)
	want := time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWindow_Deadline_SameDay(t *testing.T) {
	w := mustParseWindow(t, "03:00-05:00")
	start := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	got := w.Deadline(start)
	want := time.Date(2026, 9, 10, 5, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWindow_Deadline_CrossesMidnight(t *testing.T) {
	w := mustParseWindow(t, "23:30-01:00")
	start := time.Date(2026, 9, 10, 23, 30, 0, 0, time.UTC)
	got := w.Deadline(start)
	want := time.Date(2026, 9, 11, 1, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestIsWeeklyDeepDay(t *testing.T) {
	// Find a Sunday deterministically instead of hardcoding a calendar date.
	sunday := time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC)
	for sunday.Weekday() != time.Sunday {
		sunday = sunday.AddDate(0, 0, 1)
	}
	monday := sunday.AddDate(0, 0, 1)

	if !IsWeeklyDeepDay(sunday) {
		t.Fatal("expected Sunday to be the weekly deep day")
	}
	if IsWeeklyDeepDay(monday) {
		t.Fatal("expected Monday not to be the weekly deep day")
	}
}
