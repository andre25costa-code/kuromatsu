package cron

import "github.com/spf13/cobra"

func newDisableCommand(storePath func() string) *cobra.Command {
	return &cobra.Command{
		Use:     "disable",
		Short:   "Disable a job",
		Args:    cobra.ExactArgs(1),
		Example: `kuromatsu cron disable 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cronSetJobEnabled(cmd.OutOrStdout(), storePath(), args[0], false)
		},
	}
}
