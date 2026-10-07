package config

import (
	"os"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

const (
	legacyEnvPrefix  = "PICOCLAW_"
	currentEnvPrefix = "KUROMATSU_"
)

// applyLegacyEnvCompat copies every still-set PICOCLAW_* environment
// variable to its KUROMATSU_* equivalent, when the new name is not already
// set, so deployments configured before the Kuromatsu rebrand keep working
// (ADR-005). It is generic over variable names (not a hardcoded pairing
// table) so it covers every env-tagged config field, not just the five
// named constants in this file. Runs once per process, triggered from
// GetHome() -- see legacyEnvCompatOnce.
//
// BUG FIXED 2026-09-13: both constants above were "KUROMATSU_" (identical),
// making the whole shim a silent no-op since it was introduced -- every
// PICOCLAW_* var was left untouched because the loop only ever looked at
// vars already starting with "KUROMATSU_". Caught by the first real `go
// test ./...` this project ever ran (no Go toolchain existed locally before
// this session); TestApplyLegacyEnvCompat_CopiesUnsetVar had been failing
// for the same reason and nobody had run it to notice.
func applyLegacyEnvCompat() {
	for _, kv := range os.Environ() {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(name, legacyEnvPrefix) {
			continue
		}

		newName := currentEnvPrefix + strings.TrimPrefix(name, legacyEnvPrefix)
		if _, exists := os.LookupEnv(newName); exists {
			continue // the new name wins if both are set
		}

		if err := os.Setenv(newName, value); err != nil {
			continue
		}
		logger.WarnF(name+" is deprecated, use "+newName+" instead",
			map[string]any{"old_env": name, "new_env": newName})
	}
}
