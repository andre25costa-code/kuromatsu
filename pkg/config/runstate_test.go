package config

import (
	"path/filepath"
	"testing"
)

func TestRunstateConfig_DisabledIsANoOp(t *testing.T) {
	c := RunstateConfig{Enabled: false, StateFile: "/would/be/used/if/enabled"}
	if got := c.EffectiveStateFile("/home"); got != "" {
		t.Fatalf("EffectiveStateFile() = %q, want empty when disabled (AC-017-7)", got)
	}
	if c.EffectiveSdNotify() {
		t.Fatal("EffectiveSdNotify() = true, want false when disabled")
	}
	if c.EffectiveSkipHeartbeatWhenBusy() {
		t.Fatal("EffectiveSkipHeartbeatWhenBusy() = true, want false when disabled")
	}
}

func TestRunstateConfig_EnabledDefaults(t *testing.T) {
	c := RunstateConfig{Enabled: true}
	want := filepath.Join("/home", "run", "state")
	if got := c.EffectiveStateFile("/home"); got != want {
		t.Fatalf("EffectiveStateFile() = %q, want %q", got, want)
	}
	if !c.EffectiveSdNotify() {
		t.Fatal("EffectiveSdNotify() default = false, want true")
	}
	if !c.EffectiveSkipHeartbeatWhenBusy() {
		t.Fatal("EffectiveSkipHeartbeatWhenBusy() default = false, want true")
	}
}

func TestRunstateConfig_ExplicitOverrides(t *testing.T) {
	no := false
	c := RunstateConfig{Enabled: true, StateFile: "/custom/state", SdNotify: &no, SkipHeartbeatWhenBusy: &no}
	if got := c.EffectiveStateFile("/home"); got != "/custom/state" {
		t.Fatalf("EffectiveStateFile() = %q, want %q", got, "/custom/state")
	}
	if c.EffectiveSdNotify() {
		t.Fatal("EffectiveSdNotify() = true, want false (explicit override)")
	}
	if c.EffectiveSkipHeartbeatWhenBusy() {
		t.Fatal("EffectiveSkipHeartbeatWhenBusy() = true, want false (explicit override)")
	}
}
