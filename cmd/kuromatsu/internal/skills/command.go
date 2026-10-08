package skills

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/skills"
)

type deps struct {
	workspace    string
	skillsLoader *skills.SkillsLoader
}

func NewSkillsCommand() *cobra.Command {
	var d deps

	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := internal.LoadConfig()
			if err != nil {
				return fmt.Errorf("error loading config: %w", err)
			}

			d.workspace = cfg.WorkspacePath()

			// Same roots as the agent: workspace, global (<home>/skills) and the
			// optional KUROMATSU_BUILTIN_SKILLS directory.
			globalSkillsDir := filepath.Join(filepath.Dir(internal.GetConfigPath()), "skills")
			d.skillsLoader = skills.NewSkillsLoader(d.workspace, globalSkillsDir, config.BuiltinSkillsDir())

			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	loaderFn := func() (*skills.SkillsLoader, error) {
		if d.skillsLoader == nil {
			return nil, fmt.Errorf("skills loader is not initialized")
		}
		return d.skillsLoader, nil
	}

	workspaceFn := func() (string, error) {
		if d.workspace == "" {
			return "", fmt.Errorf("workspace is not initialized")
		}
		return d.workspace, nil
	}

	cmd.AddCommand(
		newListCommand(loaderFn),
		newInstallCommand(),
		newInstallBuiltinCommand(workspaceFn),
		newListBuiltinCommand(workspaceFn),
		newRemoveCommand(),
		newSearchCommand(),
		newShowCommand(loaderFn),
	)

	return cmd
}
