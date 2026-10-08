package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/skills"
	"github.com/andre25costa-code/kuromatsu/pkg/utils"
)

const skillsSearchMaxResults = 20

type installedSkillOriginMeta struct {
	Version          int    `json:"version"`
	OriginKind       string `json:"origin_kind,omitempty"`
	Registry         string `json:"registry,omitempty"`
	Slug             string `json:"slug,omitempty"`
	RegistryURL      string `json:"registry_url,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
	InstalledAt      int64  `json:"installed_at"`
}

func skillsListCmd(loader *skills.SkillsLoader) {
	allSkills := loader.ListSkills()

	if len(allSkills) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	fmt.Println("\nInstalled Skills:")
	fmt.Println("------------------")
	for _, skill := range allSkills {
		fmt.Printf("  ✓ %s (%s)\n", skill.Name, skill.Source)
		if skill.Description != "" {
			fmt.Printf("    %s\n", skill.Description)
		}
	}
}

// skillsInstallFromRegistry installs a skill from a named registry (e.g. clawhub).
func skillsInstallFromRegistry(cfg *config.Config, registryName, target string) error {
	err := utils.ValidateSkillIdentifier(registryName)
	if err != nil {
		return fmt.Errorf("✗  invalid registry name: %w", err)
	}

	registryMgr := skills.NewRegistryManagerFromToolsConfig(cfg.Tools.Skills)

	registry := registryMgr.GetRegistry(registryName)
	if registry == nil {
		return fmt.Errorf("✗  registry '%s' not found or not enabled. check your config.json.", registryName)
	}

	dirName, err := registry.ResolveInstallDirName(target)
	if err != nil {
		return fmt.Errorf("✗  invalid install target %q: %w", target, err)
	}

	fmt.Printf("Installing skill '%s' from %s registry...\n", target, registryName)

	workspace := cfg.WorkspacePath()
	targetDir := filepath.Join(workspace, "skills", dirName)

	if _, err = os.Stat(targetDir); err == nil {
		return fmt.Errorf("\u2717 skill '%s' already installed at %s", dirName, targetDir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err = os.MkdirAll(filepath.Join(workspace, "skills"), 0o755); err != nil {
		return fmt.Errorf("\u2717 failed to create skills directory: %w", err)
	}

	result, err := registry.DownloadAndInstall(ctx, target, "", targetDir)
	if err != nil {
		rmErr := os.RemoveAll(targetDir)
		if rmErr != nil {
			fmt.Printf("\u2717 Failed to remove partial install: %v\n", rmErr)
		}
		return fmt.Errorf("✗ failed to install skill: %w", err)
	}

	if result.IsMalwareBlocked {
		rmErr := os.RemoveAll(targetDir)
		if rmErr != nil {
			fmt.Printf("\u2717 Failed to remove partial install: %v\n", rmErr)
		}

		return fmt.Errorf("\u2717 Skill '%s' is flagged as malicious and cannot be installed.\n", target)
	}

	if result.IsSuspicious {
		fmt.Printf("\u26a0\ufe0f  Warning: skill '%s' is flagged as suspicious.\n", target)
	}

	if !workspaceHasValidSkillDirectory(workspace, dirName) {
		_ = os.RemoveAll(targetDir)
		return fmt.Errorf("✗ failed to install skill: registry archive for %q is not a valid skill", target)
	}

	normalizedSlug, registryURL := skills.BuildInstallMetadataForRegistryInstance(registry, target, result.Version)
	installedAt := time.Now().UnixMilli()
	if err := writeInstalledSkillOriginMeta(targetDir, installedSkillOriginMeta{
		Version:          1,
		OriginKind:       "third_party",
		Registry:         registry.Name(),
		Slug:             normalizedSlug,
		RegistryURL:      registryURL,
		InstalledVersion: result.Version,
		InstalledAt:      installedAt,
	}); err != nil {
		_ = os.RemoveAll(targetDir)
		return fmt.Errorf("✗ failed to persist skill metadata: %w", err)
	}

	fmt.Printf("\u2713 Skill '%s' v%s installed successfully!\n", dirName, result.Version)
	if result.Summary != "" {
		fmt.Printf("  %s\n", result.Summary)
	}

	return nil
}

func writeInstalledSkillOriginMeta(targetDir string, meta installedSkillOriginMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(filepath.Join(targetDir, ".skill-origin.json"), data, 0o600)
}

func workspaceHasValidSkillDirectory(workspace, directory string) bool {
	loader := skills.NewSkillsLoader(workspace, "", "")
	for _, skill := range loader.ListSkills() {
		if skill.Source != "workspace" {
			continue
		}
		if filepath.Base(filepath.Dir(skill.Path)) == directory {
			return true
		}
	}
	return false
}

func skillsRemoveFromWorkspace(workspace string, toolsConfig config.SkillsToolsConfig, skillName string) error {
	name := strings.TrimSpace(skillName)
	name = strings.Trim(name, "/")
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if strings.Contains(name, "/") {
		dirName, err := skills.GitHubInstallDirNameFromToolsConfig(toolsConfig, name)
		if err != nil || dirName == "" {
			return fmt.Errorf("invalid skill name %q", skillName)
		}
		name = dirName
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid skill name %q", skillName)
	}
	skillDir := filepath.Join(workspace, "skills", name)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return fmt.Errorf("skill '%s' not found", name)
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("failed to remove skill '%s': %w", name, err)
	}
	return nil
}

func skillsInstallBuiltinCmd(workspace string) error {
	installed, kept, err := installBuiltinSkills(kuromatsu.OnboardWorkspace, workspace)
	for _, name := range installed {
		fmt.Printf("✓ Installed builtin skill '%s'\n", name)
	}
	if err != nil {
		return err
	}
	if len(kept) > 0 {
		fmt.Printf("= Already in the workspace (left unchanged): %s\n", strings.Join(kept, ", "))
	}
	if len(installed) == 0 {
		fmt.Println("All builtin skills are already installed.")
	}
	return nil
}

func skillsListBuiltinCmd(workspace string) error {
	list, err := builtinSkills(kuromatsu.OnboardWorkspace)
	if err != nil {
		return err
	}
	fmt.Println("\nBuiltin skills (✓ = installed in the workspace):")
	fmt.Println("-----------------------")
	for _, s := range list {
		status := " "
		if _, statErr := os.Stat(filepath.Join(workspace, "skills", s.Name)); statErr == nil {
			status = "✓"
		}
		fmt.Printf("  %s  %s\n", status, s.Name)
		if s.Description != "" {
			fmt.Printf("     %s\n", s.Description)
		}
	}
	return nil
}

func skillsSearchCmd(query string) {
	fmt.Println("Searching for available skills...")

	cfg, err := internal.LoadConfig()
	if err != nil {
		fmt.Printf("✗ Failed to load config: %v\n", err)
		return
	}

	registryMgr := skills.NewRegistryManagerFromToolsConfig(cfg.Tools.Skills)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := registryMgr.SearchAll(ctx, query, skillsSearchMaxResults)
	if err != nil {
		fmt.Printf("✗ Failed to fetch skills list: %v\n", err)
		return
	}

	if len(results) == 0 {
		fmt.Println("No skills available.")
		return
	}

	fmt.Printf("\nAvailable Skills (%d):\n", len(results))
	fmt.Println("--------------------")
	for _, result := range results {
		fmt.Printf("  📦 %s\n", result.DisplayName)
		fmt.Printf("     %s\n", result.Summary)
		fmt.Printf("     Slug: %s\n", result.Slug)
		fmt.Printf("     Registry: %s\n", result.RegistryName)
		if result.Version != "" {
			fmt.Printf("     Version: %s\n", result.Version)
		}
		if result.RegistryName == "github" {
			fmt.Printf("     Install: kuromatsu skills install %s\n", result.Slug)
		} else {
			fmt.Printf("     Install: kuromatsu skills install --registry=%s %s\n", result.RegistryName, result.Slug)
		}
		fmt.Println()
	}
}

func skillsShowCmd(loader *skills.SkillsLoader, skillName string) {
	content, ok := loader.LoadSkill(skillName)
	if !ok {
		fmt.Printf("✗ Skill '%s' not found\n", skillName)
		return
	}

	fmt.Printf("\n📦 Skill: %s\n", skillName)
	fmt.Println("----------------------")
	fmt.Println(content)
}
