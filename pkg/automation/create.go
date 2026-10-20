package automation

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/constants"
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
// interprets times. The job reports to channel/to, which must be an
// enabled, non-internal channel of cfg and a recipient (see checkTarget).
// An identical existing job is returned with created=false. Errors caused
// by the input wrap ErrInvalid.
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
	channel, to = strings.TrimSpace(channel), strings.TrimSpace(to)
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
	if err = checkTarget(cfg, channel, to); err != nil {
		return nil, false, err
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

// checkTarget refuses a destination a template job must not have. A job's
// agent turn runs with its channel's trust: an empty channel would run as
// the internal "cli" channel, and an internal channel may run shell
// commands and command jobs, while templates feed fetched web pages to that
// turn. So the channel must be configured, enabled and not internal, and
// the recipient must be given: there is no fallback chat.
func checkTarget(cfg *config.Config, channel, to string) error {
	switch {
	case channel == "":
		return invalidf("choose the channel the automation reports to")
	case constants.IsInternalChannel(channel):
		return invalidf("channel %q is internal; choose a chat channel", channel)
	case cfg == nil:
		return invalidf("channel %q cannot be checked without a config", channel)
	}
	ch := cfg.Channels.Get(channel)
	switch {
	case ch == nil:
		return invalidf("channel %q is not configured", channel)
	case !ch.Enabled:
		return invalidf("channel %q is disabled", channel)
	case to == "":
		return invalidf("choose the recipient (chat ID) on channel %q the automation reports to", channel)
	}
	return nil
}
