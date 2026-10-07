package config

import (
	"path/filepath"
	"testing"
)

func TestMemguardConfig_Defaults(t *testing.T) {
	c := MemguardConfig{}
	if c.EffectiveGOGC() != 50 {
		t.Fatalf("EffectiveGOGC() = %d, want 50", c.EffectiveGOGC())
	}
	if c.EffectiveMarginMB() != 96 {
		t.Fatalf("EffectiveMarginMB() = %d, want 96", c.EffectiveMarginMB())
	}
	if c.EffectiveComputeBufferMB() != 96 {
		t.Fatalf("EffectiveComputeBufferMB() = %d, want 96", c.EffectiveComputeBufferMB())
	}
	if c.EffectiveMinAvailableMB() != 256 {
		t.Fatalf("EffectiveMinAvailableMB() = %d, want 256", c.EffectiveMinAvailableMB())
	}
	if c.EffectivePSISomeThreshold() != 20 {
		t.Fatalf("EffectivePSISomeThreshold() = %d, want 20", c.EffectivePSISomeThreshold())
	}
	if c.EffectivePSIFullThreshold() != 10 {
		t.Fatalf("EffectivePSIFullThreshold() = %d, want 10", c.EffectivePSIFullThreshold())
	}
	if c.EffectivePSISustainSecs() != 30 {
		t.Fatalf("EffectivePSISustainSecs() = %d, want 30", c.EffectivePSISustainSecs())
	}
}

func TestMemguardConfig_ExplicitValuesWin(t *testing.T) {
	c := MemguardConfig{
		GOGC: 75, MarginMB: 10, ComputeBufferMB: 20, MinAvailableMB: 30,
		PSISomeThreshold: 40, PSIFullThreshold: 5, PSISustainSecs: 60,
	}
	if c.EffectiveGOGC() != 75 || c.EffectiveMarginMB() != 10 || c.EffectiveComputeBufferMB() != 20 ||
		c.EffectiveMinAvailableMB() != 30 || c.EffectivePSISomeThreshold() != 40 ||
		c.EffectivePSIFullThreshold() != 5 || c.EffectivePSISustainSecs() != 60 {
		t.Fatalf("explicit MemguardConfig values were overridden by defaults: %+v", c)
	}
}

func TestTelemetryConfig_Defaults(t *testing.T) {
	c := TelemetryConfig{Enabled: false, DBPath: "/would/be/used"}
	if got := c.EffectiveDBPath("/home"); got != "" {
		t.Fatalf("EffectiveDBPath() = %q, want empty when disabled (AC-019-6)", got)
	}
	if c.EffectiveRetentionDays() != 30 {
		t.Fatalf("EffectiveRetentionDays() = %d, want 30", c.EffectiveRetentionDays())
	}
}

func TestTelemetryConfig_EnabledDefaultPath(t *testing.T) {
	c := TelemetryConfig{Enabled: true}
	want := filepath.Join("/home", "telemetry", "turns.db")
	if got := c.EffectiveDBPath("/home"); got != want {
		t.Fatalf("EffectiveDBPath() = %q, want %q", got, want)
	}
}
