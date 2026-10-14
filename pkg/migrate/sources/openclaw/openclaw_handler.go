package openclaw

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/migrate/internal"
)

// OpenclawHomeEnvVar is the environment variable that overrides the source
// openclaw home directory when migrating from openclaw to kuromatsu.
// Default: ~/.openclaw
const OpenclawHomeEnvVar = "OPENCLAW_HOME"

var providerMapping = map[string]string{
	"anthropic":  "anthropic",
	"claude":     "anthropic",
	"openai":     "openai",
	"gpt":        "openai",
	"groq":       "groq",
	"ollama":     "ollama",
	"openrouter": "openrouter",
	"deepseek":   "deepseek",
	"together":   "together",
	"mistral":    "mistral",
	"fireworks":  "fireworks",
	"google":     "google",
	"gemini":     "google",
	"xai":        "xai",
	"grok":       "xai",
	"cerebras":   "cerebras",
	"sambanova":  "sambanova",
}

type OpenclawHandler struct {
	opts             Options
	sourceConfigFile string
	sourceWorkspace  string
}

type (
	Options   = internal.Options
	Action    = internal.Action
	Result    = internal.Result
	Operation = internal.Operation
)

func NewOpenclawHandler(opts Options) (Operation, error) {
	home, err := resolveSourceHome(opts.SourceHome)
	if err != nil {
		return nil, err
	}
	opts.SourceHome = home

	configFile, err := findSourceConfig(home)
	if err != nil {
		return nil, err
	}
	return &OpenclawHandler{
		opts:             opts,
		sourceWorkspace:  filepath.Join(opts.SourceHome, "workspace"),
		sourceConfigFile: configFile,
	}, nil
}

func (o *OpenclawHandler) GetSourceName() string {
	return "openclaw"
}

func (o *OpenclawHandler) GetSourceHome() (string, error) {
	return o.opts.SourceHome, nil
}

func (o *OpenclawHandler) GetSourceWorkspace() (string, error) {
	return o.sourceWorkspace, nil
}

func (o *OpenclawHandler) GetSourceConfigFile() (string, error) {
	return o.sourceConfigFile, nil
}

func (o *OpenclawHandler) GetMigrateableFiles() []string {
	return migrateableFiles
}

func (o *OpenclawHandler) GetMigrateableDirs() []string {
	return migrateableDirs
}

// buildStandardConfig turns the converted OpenClaw config into a Kuromatsu
// config; it is a variable so tests can force an invalid result.
var buildStandardConfig = (*KuromatsuConfig).ToStandardConfig

func (o *OpenclawHandler) ExecuteConfigMigration(srcConfigPath, dstConfigPath string) error {
	openclawCfg, err := LoadOpenClawConfig(srcConfigPath)
	if err != nil {
		return err
	}

	picoCfg, warnings, err := openclawCfg.ConvertToKuromatsu(o.opts.SourceHome)
	if err != nil {
		return err
	}

	for _, w := range warnings {
		fmt.Printf("  Warning: %s\n", w)
	}

	incoming := buildStandardConfig(picoCfg)
	if err := validateMigratedConfig(incoming, filepath.Dir(dstConfigPath)); err != nil {
		return fmt.Errorf("migrated config is invalid, nothing was written: %w", err)
	}

	dstDir := filepath.Dir(dstConfigPath)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	return config.SaveConfig(dstConfigPath, incoming)
}

// validateMigratedConfig writes cfg to a scratch directory and loads it back
// with config.LoadConfig, so a config the binary would refuse is caught before
// anything reaches the destination. The load runs with the process environment,
// and relative file:// references would resolve against the scratch directory;
// the conversion emits neither enc:// nor file:// values.
func validateMigratedConfig(cfg *config.Config, dstDir string) error {
	tmpDir, err := os.MkdirTemp("", "kuromatsu-migrate-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	// LoadConfig repoints the process-wide secret resolver at the scratch
	// directory; whatever the outcome, point it back at the destination before
	// the scratch directory is removed.
	defer config.UseSecretDir(dstDir)

	tmpPath := filepath.Join(tmpDir, "config.json")
	if err = config.SaveConfig(tmpPath, cfg); err != nil {
		return err
	}
	_, err = config.LoadConfig(tmpPath)
	return err
}

func resolveSourceHome(override string) (string, error) {
	if override != "" {
		return config.ExpandHome(override), nil
	}
	if envHome := os.Getenv(OpenclawHomeEnvVar); envHome != "" {
		return config.ExpandHome(envHome), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw"), nil
}

func findSourceConfig(sourceHome string) (string, error) {
	candidates := []string{
		filepath.Join(sourceHome, "openclaw.json"),
		filepath.Join(sourceHome, "config.json"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no config file found in %s (tried openclaw.json, config.json)", sourceHome)
}

func rewriteWorkspacePath(path string) string {
	path = strings.Replace(path, ".openclaw", ".kuromatsu", 1)
	return path
}

func mapProvider(provider string) string {
	if mapped, ok := providerMapping[strings.ToLower(provider)]; ok {
		return mapped
	}
	return strings.ToLower(provider)
}
