package skills

import "github.com/spf13/cobra"

func newListBuiltinCommand(workspaceFn func() (string, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list-builtin",
		Short:   "List available builtin skills",
		Example: `kuromatsu skills list-builtin`,
		RunE: func(_ *cobra.Command, _ []string) error {
			workspace, err := workspaceFn()
			if err != nil {
				return err
			}
			return skillsListBuiltinCmd(workspace)
		},
	}

	return cmd
}
