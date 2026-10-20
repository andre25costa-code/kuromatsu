package cron

import "github.com/spf13/cobra"

func newEnableCommand(storePath func() string) *cobra.Command {
	return &cobra.Command{
		Use:     "enable",
		Short:   "Enable a job",
		Args:    cobra.ExactArgs(1),
		Example: `kuromatsu cron enable 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cronSetJobEnabled(cmd.OutOrStdout(), storePath(), args[0], true)
		},
	}
}
