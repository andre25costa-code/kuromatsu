package internal

import (
	"os"
	"path/filepath"

	"github.com/andre25costa-code/kuromatsu/pkg"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

const Logo = pkg.Logo

// GetKuromatsuHome returns the kuromatsu home directory.
// Priority: $KUROMATSU_HOME > ~/.kuromatsu
func GetKuromatsuHome() string {
	return config.GetHome()
}

func GetConfigPath() string {
	if configPath := os.Getenv(config.EnvConfig); configPath != "" {
		return configPath
	}
	return filepath.Join(GetKuromatsuHome(), "config.json")
}

// LoadConfig loads the runtime config and, the first time in the process,
// applies its timezone (see config.ApplyProcessTimezone).
func LoadConfig() (*config.Config, error) {
	cfg, err := config.LoadConfig(GetConfigPath())
	if err != nil {
		return nil, err
	}
	logger.SetLevelFromString(cfg.Gateway.LogLevel)
	config.ApplyProcessTimezone(cfg)
	return cfg, nil
}

// LoadConfigForEdit loads the config as stored on disk, without environment
// overrides or runtime defaults, for commands that change and save it. Like
// LoadConfig, it applies the file's gateway.log_level.
func LoadConfigForEdit() (*config.Config, error) {
	cfg, err := config.LoadConfigForEdit(GetConfigPath())
	if err != nil {
		return nil, err
	}
	logger.SetLevelFromString(cfg.Gateway.LogLevel)
	return cfg, nil
}
