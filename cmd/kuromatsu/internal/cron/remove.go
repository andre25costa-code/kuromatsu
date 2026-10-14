package cron

import "github.com/spf13/cobra"

func newRemoveCommand(storePath func() string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove",
		Short:   "Remove a job by ID",
		Args:    cobra.ExactArgs(1),
		Example: `kuromatsu cron remove 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cronRemoveCmd(cmd.OutOrStdout(), storePath(), args[0])
			return nil
		},
	}

	return cmd
}
