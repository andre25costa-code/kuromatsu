package skills

import "github.com/spf13/cobra"

func newInstallBuiltinCommand(workspaceFn func() (string, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "install-builtin",
		Short:   "Install the builtin skills missing from the workspace",
		Example: `kuromatsu skills install-builtin`,
		RunE: func(_ *cobra.Command, _ []string) error {
			workspace, err := workspaceFn()
			if err != nil {
				return err
			}
			return skillsInstallBuiltinCmd(workspace)
		},
	}

	return cmd
}
