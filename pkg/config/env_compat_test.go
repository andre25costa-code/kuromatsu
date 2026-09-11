package config

import (
	"os"
	"testing"
)

// These tests call applyLegacyEnvCompat directly rather than through
// GetHome(), which only runs it once per process via sync.Once -- calling
// the function itself keeps each test's env setup independent.

func TestApplyLegacyEnvCompat_CopiesUnsetVar(t *testing.T) {
	t.Setenv("KUROMATSU_TEST_ENV_COMPAT_A", "old-value")
	os.Unsetenv("KUROMATSU_TEST_ENV_COMPAT_A")

	applyLegacyEnvCompat()

	if got := os.Getenv("KUROMATSU_TEST_ENV_COMPAT_A"); got != "old-value" {
		t.Fatalf("KUROMATSU_TEST_ENV_COMPAT_A = %q, want %q", got, "old-value")
	}
}

func TestApplyLegacyEnvCompat_NewNameWins(t *testing.T) {
	t.Setenv("KUROMATSU_TEST_ENV_COMPAT_B", "old-value")
	t.Setenv("KUROMATSU_TEST_ENV_COMPAT_B", "new-value")

	applyLegacyEnvCompat()

	if got := os.Getenv("KUROMATSU_TEST_ENV_COMPAT_B"); got != "new-value" {
		t.Fatalf("KUROMATSU_TEST_ENV_COMPAT_B = %q, want unchanged %q", got, "new-value")
	}
}

func TestApplyLegacyEnvCompat_IgnoresUnrelatedVars(t *testing.T) {
	t.Setenv("SOME_OTHER_VAR", "x")
	applyLegacyEnvCompat() // must not panic or touch unrelated vars
	if got := os.Getenv("KUROMATSU_SOME_OTHER_VAR"); got != "" {
		t.Fatalf("unexpected var created: %q", got)
	}
}
