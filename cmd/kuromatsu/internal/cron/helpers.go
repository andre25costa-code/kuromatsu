package cron

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func cronListCmd(w io.Writer, storePath string) {
	cs := cron.NewCronService(storePath, nil)
	jobs := cs.ListJobs(true) // Show all jobs, including disabled

	if len(jobs) == 0 {
		fmt.Fprintln(w, "No scheduled jobs.")
		return
	}

	fmt.Fprintln(w, "\nScheduled Jobs:")
	fmt.Fprintln(w, "----------------")
	for _, job := range jobs {
		var schedule string
		if job.Schedule.Kind == "every" && job.Schedule.EveryMS != nil {
			schedule = fmt.Sprintf("every %ds", *job.Schedule.EveryMS/1000)
		} else if job.Schedule.Kind == "cron" {
			schedule = job.Schedule.Expr
		} else {
			schedule = "one-time"
		}
		if job.Schedule.TZ != "" {
			schedule += " (" + job.Schedule.TZ + ")"
		}

		nextRun := "-"
		if job.State.NextRunAtMS != nil {
			nextTime := time.UnixMilli(*job.State.NextRunAtMS)
			nextRun = nextTime.Format("2006-01-02 15:04")
		}

		status := "enabled"
		if !job.Enabled {
			status = "disabled"
		}

		lastRun := "-"
		if job.State.LastStatus != "" {
			lastRun = job.State.LastStatus
			if job.State.LastError != "" {
				lastRun += ": " + job.State.LastError
			}
		}

		fmt.Fprintf(w, "  %s (%s)\n", job.Name, job.ID)
		fmt.Fprintf(w, "    Schedule: %s\n", schedule)
		fmt.Fprintf(w, "    Status: %s\n", status)
		fmt.Fprintf(w, "    Next run: %s\n", nextRun)
		fmt.Fprintf(w, "    Last run: %s\n", lastRun)
	}
}

func cronRemoveCmd(w io.Writer, storePath, jobID string) {
	cs := cron.NewCronService(storePath, nil)
	if cs.RemoveJob(jobID) {
		fmt.Fprintf(w, "✓ Removed job %s\n", jobID)
	} else {
		fmt.Fprintf(w, "✗ Job %s not found\n", jobID)
	}
}

func cronSetJobEnabled(w io.Writer, storePath, jobID string, enabled bool) error {
	cs := cron.NewCronService(storePath, nil)
	job, err := cs.EnableJob(jobID, enabled)
	if errors.Is(err, cron.ErrJobNotFound) {
		return fmt.Errorf("job %s not found", jobID)
	}
	if err != nil {
		return fmt.Errorf("job %s: %w", jobID, err)
	}
	status := "enabled"
	if !enabled {
		status = "disabled"
	}
	fmt.Fprintf(w, "✓ Job '%s' %s\n", job.Name, status)
	return nil
}
