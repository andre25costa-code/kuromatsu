package internal

import (
	"os"
	"path/filepath"

	"github.com/andre25costa-code/kuromatsu/pkg"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

const Logo = pkg.Logo

// GetPicoclawHome returns the kuromatsu home directory.
// Priority: $KUROMATSU_HOME > ~/.kuromatsu
func GetPicoclawHome() string {
	return config.GetHome()
}

func GetConfigPath() string {
	if configPath := os.Getenv(config.EnvConfig); configPath != "" {
		return configPath
	}
	return filepath.Join(GetPicoclawHome(), "config.json")
}

func LoadConfig() (*config.Config, error) {
	cfg, err := config.LoadConfig(GetConfigPath())
	if err != nil {
		return nil, err
	}
	logger.SetLevelFromString(cfg.Gateway.LogLevel)
	return cfg, nil
}
