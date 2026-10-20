package cron

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func newHistoryCommand(storePath func() string) *cobra.Command {
	return &cobra.Command{
		Use:     "history",
		Short:   "Show a job's recent runs",
		Args:    cobra.ExactArgs(1),
		Example: `kuromatsu cron history 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read-only: the run history is written by the gateway alone.
			cs := cron.NewCronService(storePath(), nil)
			job, ok := cs.GetJob(args[0])
			if !ok {
				return fmt.Errorf("job %s not found", args[0])
			}
			fmt.Fprint(cmd.OutOrStdout(), cron.FormatHistory(job, cs.History(job.ID)))
			return nil
		},
	}
}
