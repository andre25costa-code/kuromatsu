package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSysmonTool_UnknownAction(t *testing.T) {
	tool := NewSysmonTool(false)
	res := tool.Execute(context.Background(), map[string]any{"action": "bogus"})
	if !res.IsError {
		t.Fatal("expected an error result for an unknown action")
	}
}

func TestSysmonTool_ProcRequiresPid(t *testing.T) {
	tool := NewSysmonTool(true)
	res := tool.Execute(context.Background(), map[string]any{"action": "proc", "op": "kill"})
	if !res.IsError || !strings.Contains(res.ForLLM, "pid") {
		t.Fatalf("expected a pid-required error, got %+v", res)
	}
}

func TestSysmonTool_ProcRequiresOp(t *testing.T) {
	tool := NewSysmonTool(true)
	res := tool.Execute(context.Background(), map[string]any{"action": "proc", "pid": 1})
	if !res.IsError || !strings.Contains(res.ForLLM, "op") {
		t.Fatalf("expected an op-required error, got %+v", res)
	}
}

// AC-011-2: destructive actions are denied by default.
func TestSysmonTool_KillDeniedByDefault(t *testing.T) {
	tool := NewSysmonTool(false)
	res := tool.Execute(context.Background(), map[string]any{"action": "proc", "op": "kill", "pid": 999999})
	if !res.IsError {
		t.Fatal("expected kill to be denied")
	}
	if !strings.Contains(res.ForLLM, "desabilitada") {
		t.Fatalf("expected a disabled-action message, got: %s", res.ForLLM)
	}
}

func TestSysmonTool_ReniceDeniedByDefault(t *testing.T) {
	tool := NewSysmonTool(false)
	res := tool.Execute(context.Background(), map[string]any{"action": "proc", "op": "renice", "pid": 999999, "niceness": 5})
	if !res.IsError {
		t.Fatal("expected renice to be denied")
	}
}

func TestSysmonTool_KillAllowedWhenConfigured_InvalidPidStillErrors(t *testing.T) {
	tool := NewSysmonTool(true)
	// A pid that (almost certainly) doesn't exist: exercises the "allowed"
	// code path without actually killing anything real.
	res := tool.Execute(context.Background(), map[string]any{"action": "proc", "op": "kill", "pid": 999999})
	if !res.IsError {
		t.Fatal("expected an error killing a nonexistent pid")
	}
	if strings.Contains(res.ForLLM, "desabilitada") {
		t.Fatal("should not be the disabled-action message once AllowDestructive is true")
	}
}

func TestSysmonTool_InvalidLimit(t *testing.T) {
	tool := NewSysmonTool(false)
	res := tool.Execute(context.Background(), map[string]any{"action": "top", "limit": -1})
	if !res.IsError {
		t.Fatal("expected an error for a non-positive limit")
	}
}

func TestSysmonTool_InvalidNiceness(t *testing.T) {
	tool := NewSysmonTool(true)
	res := tool.Execute(context.Background(), map[string]any{"action": "proc", "op": "renice", "pid": 1, "niceness": 100})
	if !res.IsError {
		t.Fatal("expected an error for niceness out of [-20,19]")
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[uint64]string{
		0:                 "0 B",
		1023:              "1023 B",
		1024:              "1.0 KiB",
		1024 * 1024:       "1.0 MiB",
		1536 * 1024 * 1024: "1.5 GiB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestToInt(t *testing.T) {
	cases := []struct {
		in      any
		want    int
		wantErr bool
	}{
		{42, 42, false},
		{int64(7), 7, false},
		{float64(3), 3, false},
		{"15", 15, false},
		{" 9 ", 9, false},
		{"nope", 0, true},
		{true, 0, true}, // unsupported type
	}
	for _, c := range cases {
		got, err := toInt(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("toInt(%v): expected error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("toInt(%v) = %d, %v; want %d, nil", c.in, got, err, c.want)
		}
	}
}
