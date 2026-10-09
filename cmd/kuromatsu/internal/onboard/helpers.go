package onboard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/term"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/cliui"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/credential"
)

func onboard(encrypt, force bool) {
	configPath := internal.GetConfigPath()

	configExists := false
	if _, err := os.Stat(configPath); err == nil {
		configExists = true
		if encrypt {
			// Only ask for confirmation when *both* config and SSH key already exist,
			// indicating a full re-onboard that would reset the config to defaults.
			sshKeyPath, _ := credential.DefaultSSHKeyPath()
			if _, err := os.Stat(sshKeyPath); err == nil {
				// Both exist — confirm a full reset.
				fmt.Printf("Config already exists at %s\n", configPath)
				fmt.Print("Overwrite config with defaults? (y/n): ")
				var response string
				fmt.Scanln(&response)
				if response != "y" {
					fmt.Println("Aborted.")
					return
				}
				configExists = false // user agreed to reset; treat as fresh
			}
			// Config exists but SSH key is missing — keep existing config, only add SSH key.
		}
	}

	var err error
	if encrypt {
		fmt.Println("\nSet up credential encryption")
		fmt.Println("-----------------------------")
		passphrase, pErr := promptPassphrase()
		if pErr != nil {
			fmt.Printf("Error: %v\n", pErr)
			os.Exit(1)
		}
		// Expose the passphrase to credential.PassphraseProvider (which calls
		// os.Getenv by default) so that SaveConfig can encrypt api_keys.
		// This process is a one-shot CLI tool; the env var is never exposed outside
		// the current process and disappears when it exits.
		os.Setenv(credential.PassphraseEnvVar, passphrase)

		if err = setupSSHKey(); err != nil {
			fmt.Printf("Error generating SSH key: %v\n", err)
			os.Exit(1)
		}
	}

	var cfg *config.Config
	if configExists {
		// Preserve the existing config; SaveConfig will re-encrypt api_keys with the new passphrase.
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			fmt.Printf("Error loading existing config: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg = config.DefaultConfig()
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	workspace := cfg.WorkspacePath()
	createWorkspaceTemplates(workspace, force)

	cliui.PrintOnboardComplete(internal.Logo, encrypt, configPath)
}

// promptPassphrase reads the encryption passphrase twice from the terminal
// (with echo disabled) and returns it. Returns an error if the passphrase is
// empty or if the two inputs do not match.
func promptPassphrase() (string, error) {
	fmt.Print("Enter passphrase for credential encryption: ")
	p1, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	if len(p1) == 0 {
		return "", fmt.Errorf("passphrase must not be empty")
	}

	fmt.Print("Confirm passphrase: ")
	p2, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading passphrase confirmation: %w", err)
	}

	if string(p1) != string(p2) {
		return "", fmt.Errorf("passphrases do not match")
	}
	return string(p1), nil
}

// setupSSHKey generates the credential-encryption SSH key at
// ~/.ssh/picoclaw_ed25519.key (name kept from PicoClaw so existing encrypted
// credentials keep working; see credential.DefaultSSHKeyPath).
// If the key already exists the user is warned and asked to confirm overwrite.
// Answering anything other than "y" keeps the existing key (not an error).
func setupSSHKey() error {
	keyPath, err := credential.DefaultSSHKeyPath()
	if err != nil {
		return fmt.Errorf("cannot determine SSH key path: %w", err)
	}

	if _, err := os.Stat(keyPath); err == nil {
		fmt.Printf("\n⚠️  WARNING: %s already exists.\n", keyPath)
		fmt.Println("    Overwriting will invalidate any credentials previously encrypted with this key.")
		fmt.Print("    Overwrite? (y/n): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" {
			fmt.Println("Keeping existing SSH key.")
			return nil
		}
	}

	if err := credential.GenerateSSHKey(keyPath); err != nil {
		return err
	}
	fmt.Printf("SSH key generated: %s\n", keyPath)
	return nil
}

func createWorkspaceTemplates(workspace string, force bool) {
	res, err := copyEmbeddedToTarget(workspace, force)
	if err != nil {
		fmt.Printf("Error copying workspace templates: %v\n", err)
	}
	if len(res.kept) > 0 {
		fmt.Printf("Kept %d existing workspace file(s) in %s (use --force to restore the templates).\n",
			len(res.kept), workspace)
	}
	if len(res.skipped) > 0 {
		names := make([]string, 0, len(res.skipped))
		for name := range res.skipped {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Println("Skills not installed (this machine lacks what they need):")
		for _, name := range names {
			fmt.Printf("  %s: %s\n", name, strings.Join(res.skipped[name], ", "))
		}
		fmt.Println("Install the tools, then run `kuromatsu skills install-builtin`.")
	}
}

// copyResult reports what copyEmbeddedToTarget left alone: workspace files
// that already existed (kept), and builtin skills not installed because this
// machine lacks what they need (skipped: skill -> missing tools/OS).
type copyResult struct {
	kept    []string
	skipped map[string][]string
}

func copyEmbeddedToTarget(targetDir string, overwrite bool) (res copyResult, err error) {
	// Ensure target directory exists
	if mkErr := os.MkdirAll(targetDir, 0o755); mkErr != nil {
		return res, fmt.Errorf("Failed to create target directory: %w", mkErr)
	}

	// Walk through all files in embed.FS
	err = fs.WalkDir(embeddedFiles, "workspace", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// A builtin skill whose tools are missing here is not installed
			// (unless the user already has it); `skills install-builtin`
			// installs it on request.
			if filepath.ToSlash(filepath.Dir(path)) == "workspace/skills" {
				if _, statErr := os.Stat(filepath.Join(targetDir, "skills", d.Name())); statErr != nil {
					if missing := skillMissing(embeddedFiles, path); len(missing) > 0 {
						if res.skipped == nil {
							res.skipped = map[string][]string{}
						}
						res.skipped[d.Name()] = missing
						return fs.SkipDir
					}
				}
			}
			return nil
		}

		// Read embedded file
		data, err := embeddedFiles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Failed to read embedded file %s: %w", path, err)
		}

		new_path, err := filepath.Rel("workspace", path)
		if err != nil {
			return fmt.Errorf("Failed to get relative path for %s: %v\n", path, err)
		}

		// Build target file path
		targetPath := filepath.Join(targetDir, new_path)

		// Ensure target file's directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("Failed to create directory %s: %w", filepath.Dir(targetPath), err)
		}

		// The workspace belongs to the user once it exists: re-running
		// onboard (e.g. `onboard --enc`) must not replace SOUL.md, USER.md
		// or memory/MEMORY.md with the templates. Only --force does.
		if !overwrite {
			if _, statErr := os.Stat(targetPath); statErr == nil {
				res.kept = append(res.kept, filepath.ToSlash(new_path))
				return nil
			}
		}

		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			return fmt.Errorf("Failed to write file %s: %w", targetPath, err)
		}

		return nil
	})

	return res, err
}
