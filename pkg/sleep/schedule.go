package sleep

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Window is a daily time-of-day range, e.g. "03:00-05:00".
type Window struct {
	StartHour, StartMinute int
	EndHour, EndMinute     int
}

// ParseWindow parses "HH:MM-HH:MM".
func ParseWindow(s string) (Window, error) {
	start, end, ok := strings.Cut(s, "-")
	if !ok {
		return Window{}, fmt.Errorf("sleep: invalid window %q, want \"HH:MM-HH:MM\"", s)
	}
	startH, startM, err := parseHHMM(start)
	if err != nil {
		return Window{}, err
	}
	endH, endM, err := parseHHMM(end)
	if err != nil {
		return Window{}, err
	}
	return Window{startH, startM, endH, endM}, nil
}

func parseHHMM(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, fmt.Errorf("sleep: invalid time %q, want \"HH:MM\"", s)
	}
	hour, err := strconv.Atoi(h)
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("sleep: invalid hour in %q", s)
	}
	minute, err := strconv.Atoi(m)
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("sleep: invalid minute in %q", s)
	}
	return hour, minute, nil
}

// NextStart returns the next time at or after now that the window starts.
func (w Window) NextStart(now time.Time) time.Time {
	candidate := time.Date(now.Year(), now.Month(), now.Day(), w.StartHour, w.StartMinute, 0, 0, now.Location())
	if candidate.After(now) {
		return candidate
	}
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), w.StartHour, w.StartMinute, 0, 0, now.Location())
}

// Deadline returns the window's end time relative to a given start,
// handling windows that cross midnight (e.g. "23:30-01:00").
func (w Window) Deadline(start time.Time) time.Time {
	end := time.Date(start.Year(), start.Month(), start.Day(), w.EndHour, w.EndMinute, 0, 0, start.Location())
	if !end.After(start) {
		end = end.AddDate(0, 0, 1)
	}
	return end
}

// IsWeeklyDeepDay reports whether now falls on the weekly deep-reorganization day.
func IsWeeklyDeepDay(now time.Time) bool {
	return now.Weekday() == time.Sunday
}
