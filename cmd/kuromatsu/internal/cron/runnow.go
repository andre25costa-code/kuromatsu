package cron

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func newRunNowCommand(storePath func() string) *cobra.Command {
	return &cobra.Command{
		Use:     "run-now",
		Short:   "Run a job at the gateway's next wake-up",
		Args:    cobra.ExactArgs(1),
		Example: `kuromatsu cron run-now 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Only marks the job as due; the gateway's scheduler runs it.
			cs := cron.NewCronService(storePath(), nil)
			if err := cs.RunNow(args[0]); err != nil {
				return fmt.Errorf("job %s: %w", args[0], err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Job %s scheduled to run now; the gateway must be running\n", args[0])
			return nil
		},
	}
}
