package cron

import "github.com/spf13/cobra"

func newListCommand(storePath func() string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all scheduled jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cronListCmd(cmd.OutOrStdout(), storePath())
			return nil
		},
	}

	return cmd
}
