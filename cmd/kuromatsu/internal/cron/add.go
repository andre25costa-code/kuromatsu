package cron

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func newAddCommand(storePath func() string) *cobra.Command {
	var (
		name    string
		message string
		command string
		every   int64
		cronExp string
		channel string
		to      string

		quiet            bool
		anomalyRegex     string
		onAnomaly        string
		anomalyWindow    string
		anomalyMaxPerDay int
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new scheduled job",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if every <= 0 && cronExp == "" {
				return fmt.Errorf("either --every or --cron must be specified")
			}
			if command == "" && (quiet || anomalyRegex != "" || onAnomaly != "") {
				return fmt.Errorf("--quiet, --anomaly-regex and --on-anomaly only apply to --command jobs")
			}

			var schedule cron.CronSchedule
			if every > 0 {
				everyMS := every * 1000
				schedule = cron.CronSchedule{Kind: "every", EveryMS: &everyMS}
			} else {
				schedule = cron.CronSchedule{Kind: "cron", Expr: cronExp}
			}

			cs := cron.NewCronService(storePath(), nil)
			existingIDs := map[string]bool{}
			for _, existing := range cs.ListJobs(true) {
				existingIDs[existing.ID] = true
			}
			job, err := cs.AddJob(name, schedule, message, channel, to)
			if err != nil {
				return fmt.Errorf("error adding job: %w", err)
			}
			// AC-028-7: a command job runs through the exec tool at fire
			// time with no LLM turn (pkg/tools/cron.go ExecuteJob).
			if existingIDs[job.ID] && job.Payload.Command != command {
				// AddJob returned an existing identical job (AC-028-6);
				// "add" never mutates it.
				return fmt.Errorf("job '%s' (%s) already exists with command %q; remove it first",
					job.Name, job.ID, job.Payload.Command)
			}
			if existingIDs[job.ID] {
				fmt.Printf("= Job '%s' (%s) already exists; unchanged\n", job.Name, job.ID)
				return nil
			}
			if command != "" {
				job.Payload.Command = command
				// AC-028-1..3: silent on success, one bounded LLM turn on anomaly.
				job.Payload.Quiet = quiet
				job.Payload.AnomalyRegex = anomalyRegex
				if onAnomaly != "" {
					job.Payload.OnAnomaly = &cron.CronAnomaly{
						Message:   onAnomaly,
						Window:    anomalyWindow,
						MaxPerDay: anomalyMaxPerDay,
					}
				}
				if err := cs.UpdateJob(job); err != nil {
					return fmt.Errorf("error saving command for job %s: %w", job.ID, err)
				}
			}

			fmt.Printf("✓ Added job '%s' (%s)\n", job.Name, job.ID)

			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Job name")
	cmd.Flags().StringVarP(&message, "message", "m", "", "Message for agent")
	cmd.Flags().StringVar(&command, "command", "", "Shell command run without an LLM turn (output sent to --channel/--to)")
	cmd.Flags().Int64VarP(&every, "every", "e", 0, "Run every N seconds")
	cmd.Flags().StringVarP(&cronExp, "cron", "c", "", "Cron expression (e.g. '0 9 * * *')")
	cmd.Flags().StringVar(&to, "to", "", "Recipient for delivery")
	cmd.Flags().StringVar(&channel, "channel", "", "Channel for delivery")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Publish nothing when the command succeeds (only anomalies)")
	cmd.Flags().StringVar(&anomalyRegex, "anomaly-regex", "", "Treat output matching this regex as an anomaly even on exit code 0")
	cmd.Flags().StringVar(&onAnomaly, "on-anomaly", "", "Instruction for one LLM turn on anomaly (default: publish raw output)")
	cmd.Flags().StringVar(&anomalyWindow, "anomaly-window", "", "Focus window for the on-anomaly LLM turn")
	cmd.Flags().IntVar(&anomalyMaxPerDay, "anomaly-max-per-day", 0,
		fmt.Sprintf("Max on-anomaly LLM turns per day (default %d)", cron.DefaultAnomalyMaxPerDay))

	_ = cmd.MarkFlagRequired("name")
	cmd.MarkFlagsOneRequired("message", "command")
	cmd.MarkFlagsMutuallyExclusive("message", "command")
	cmd.MarkFlagsMutuallyExclusive("every", "cron")

	return cmd
}
