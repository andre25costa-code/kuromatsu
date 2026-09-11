// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/andre25costa-code/kuromatsu/pkg"
)

var legacyEnvCompatOnce sync.Once

// Runtime environment variable keys for the kuromatsu process.
// These control the location of files and binaries at runtime and are read
// directly via os.Getenv / os.LookupEnv. All kuromatsu-specific keys use the
// KUROMATSU_ prefix (legacy PICOCLAW_* values are honored via
// applyLegacyEnvCompat, ADR-005). Reference these constants instead of
// inline string literals to keep all supported knobs visible in one place
// and to prevent typos.
const (
	// EnvHome overrides the base directory for all kuromatsu data
	// (config, workspace, skills, auth store, …).
	// Default: ~/.kuromatsu (or ~/.picoclaw, read in place, if only that exists)
	EnvHome = "KUROMATSU_HOME"

	// EnvConfig overrides the full path to the JSON config file.
	// Default: $KUROMATSU_HOME/config.json
	EnvConfig = "KUROMATSU_CONFIG"

	// EnvBuiltinSkills overrides the directory from which built-in
	// skills are loaded.
	// Default: <cwd>/skills
	EnvBuiltinSkills = "KUROMATSU_BUILTIN_SKILLS"

	// EnvBinary overrides the path to the kuromatsu executable.
	// Default: resolved from the same directory as the current executable.
	EnvBinary = "KUROMATSU_BINARY"

	// EnvGatewayHost overrides the host address for the gateway server.
	// Default: "localhost"
	EnvGatewayHost = "KUROMATSU_GATEWAY_HOST"
)

// GetHome resolves the base directory for all Kuromatsu data (config,
// workspace, skills, auth store, ...). GetHome is typically the very first
// thing any entry point calls (to find the config file path itself), so it
// is where the legacy-env compat shim (ADR-005) fires exactly once.
func GetHome() string {
	legacyEnvCompatOnce.Do(applyLegacyEnvCompat)

	if home := os.Getenv(EnvHome); home != "" {
		return home
	}

	userHome, _ := os.UserHomeDir()
	if userHome == "" {
		return "."
	}

	newHome := filepath.Join(userHome, pkg.DefaultPicoClawHome)
	oldHome := filepath.Join(userHome, ".picoclaw")
	if !dirExists(newHome) && dirExists(oldHome) {
		// Read the pre-rebrand home in place rather than copying it: cheap,
		// and correct for a 1GB-RAM deploy target (S32). A dedicated
		// migration command can do a real move later if the user wants one.
		return oldHome
	}
	return newHome
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
