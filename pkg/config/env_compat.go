package config

import (
	"os"
	"strings"
	"sync"

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

// renamedEnvKeys maps an env name that was standardized to its replacement.
var renamedEnvKeys = map[string]string{
	"KUROMATSU_MAX_SEARCH_RESULTS": "KUROMATSU_TOOLS_DISCOVERY_MAX_SEARCH_RESULTS",
}

// renamedEnvWarned holds one *sync.Once per old name so the deprecation
// warning is logged once per process even though the config reloads.
var renamedEnvWarned sync.Map

// applyRenamedEnvKeys copies a still-set old name to its replacement when the
// replacement is unset (the new name wins if both are set). The value is
// copied on every call; only the warning is once per name.
func applyRenamedEnvKeys() {
	for oldName, newName := range renamedEnvKeys {
		value, ok := os.LookupEnv(oldName)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(newName); exists {
			continue
		}
		if err := os.Setenv(newName, value); err != nil {
			continue
		}
		once, _ := renamedEnvWarned.LoadOrStore(oldName, new(sync.Once))
		once.(*sync.Once).Do(func() {
			logger.WarnF(oldName+" is deprecated, use "+newName+" instead",
				map[string]any{"old_env": oldName, "new_env": newName})
		})
	}
}
