package cron

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func newEditCommand(storePath func() string) *cobra.Command {
	var (
		name    string
		message string
		command string
		every   int64
		cronExp string
		tz      string
	)

	cmd := &cobra.Command{
		Use:     "edit",
		Short:   "Edit a job's name, message, command or schedule",
		Args:    cobra.ExactArgs(1),
		Example: `kuromatsu cron edit 1 --cron "0 7 * * *" --tz America/Sao_Paulo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			if flags.Changed("tz") && !flags.Changed("cron") {
				return errors.New("--tz only applies to --cron")
			}

			cs := cron.NewCronService(storePath(), nil)
			job, ok := cs.GetJob(args[0])
			if !ok {
				return fmt.Errorf("job %s not found", args[0])
			}

			changed := false
			if flags.Changed("name") {
				job.Name = name
				changed = true
			}
			if flags.Changed("message") {
				job.Payload.Message = message
				changed = true
			}
			if flags.Changed("command") {
				job.Payload.Command = command
				changed = true
			}
			if flags.Changed("every") {
				if every <= 0 {
					return errors.New("--every must be a positive number of seconds")
				}
				everyMS := every * 1000
				job.Schedule = cron.CronSchedule{Kind: "every", EveryMS: &everyMS}
				job.DeleteAfterRun = false
				changed = true
			}
			if flags.Changed("cron") {
				job.Schedule = cron.CronSchedule{Kind: "cron", Expr: cronExp, TZ: tz}
				job.DeleteAfterRun = false
				changed = true
			}
			if !changed {
				return errors.New("nothing to edit: pass --name, --message, --command, --every or --cron")
			}

			if err := cs.UpdateJob(job); err != nil {
				return fmt.Errorf("error editing job: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Updated job '%s' (%s)\n", job.Name, job.ID)
			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "New job name")
	cmd.Flags().StringVarP(&message, "message", "m", "", "New message for the agent")
	cmd.Flags().StringVar(&command, "command", "", "New shell command (empty string clears it)")
	cmd.Flags().Int64VarP(&every, "every", "e", 0, "Run every N seconds")
	cmd.Flags().StringVarP(&cronExp, "cron", "c", "", "Cron expression (e.g. '0 9 * * *')")
	cmd.Flags().StringVar(&tz, "tz", "", "IANA time zone for --cron (e.g. America/Sao_Paulo)")
	cmd.MarkFlagsMutuallyExclusive("every", "cron")

	return cmd
}
