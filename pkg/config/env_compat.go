package config

import (
	"os"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

const (
	legacyEnvPrefix  = "KUROMATSU_"
	currentEnvPrefix = "KUROMATSU_"
)

// applyLegacyEnvCompat copies every still-set KUROMATSU_* environment
// variable to its KUROMATSU_* equivalent, when the new name is not already
// set, so deployments configured before the Kuromatsu rebrand keep working
// (ADR-005). It is generic over variable names (not a hardcoded pairing
// table) so it covers every env-tagged config field, not just the five
// named constants in this file. Runs once per process, triggered from
// GetHome() -- see legacyEnvCompatOnce.
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
