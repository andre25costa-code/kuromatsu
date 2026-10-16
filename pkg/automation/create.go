package automation

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

// ErrInvalid marks a CreateJob error caused by the request (unknown
// template, unmet requirement, bad parameter), as opposed to a failure to
// store the job: errors.Is(err, ErrInvalid). The message is the cause's.
var ErrInvalid = errors.New("invalid automation")

type invalidError struct{ err error }

func (e invalidError) Error() string        { return e.err.Error() }
func (e invalidError) Unwrap() error        { return e.err }
func (e invalidError) Is(target error) bool { return target == ErrInvalid }

func invalidf(format string, args ...any) error {
	return invalidError{fmt.Errorf(format, args...)}
}

// CreateJob renders template id with params and adds the job to cs, as the
// /cron chat command and the web UI do. cfg is the running config: its
// switches decide Unmet and its timezone (or time.Local when empty)
// interprets times. An identical existing job is returned with
// created=false. Errors caused by the input wrap ErrInvalid.
func CreateJob(
	cs *cron.CronService,
	cfg *config.Config,
	id string,
	params map[string]string,
	now time.Time,
	channel, to string,
) (job *cron.CronJob, created bool, err error) {
	if cs == nil {
		return nil, false, errors.New("scheduler is not running")
	}
	t, ok := Get(id)
	if !ok {
		return nil, false, invalidf("unknown template %q", id)
	}
	if missing := Unmet(t, cfg); len(missing) > 0 {
		return nil, false, invalidf(
			"template %s needs %s set to true in the config", id, strings.Join(missing, ", "))
	}
	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		l, lerr := time.LoadLocation(cfg.Timezone)
		if lerr != nil {
			return nil, false, fmt.Errorf("timezone %q: %w", cfg.Timezone, lerr)
		}
		loc = l
	}
	name, sched, payload, deleteAfterRun, err := Render(id, params, loc, now, channel, to)
	if err != nil {
		return nil, false, invalidError{err}
	}
	job, created, err = cs.AddJobWithPayload(name, sched, payload)
	if err != nil {
		return nil, false, err
	}
	// The scheduler already deletes one-shot ('at') jobs after their run;
	// keep the template's word authoritative if that ever changes.
	if created && deleteAfterRun && !job.DeleteAfterRun {
		job.DeleteAfterRun = true
		if err = cs.UpdateJob(job); err != nil {
			return nil, false, err
		}
	}
	return job, created, nil
}
