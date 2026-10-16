package agent

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/automation"
	"github.com/andre25costa-code/kuromatsu/pkg/commands"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

// minJobIDPrefix is the shortest job ID prefix /cron accepts.
const minJobIDPrefix = 4

// cronCommands adapts the scheduler to commands.CronAPI. Which chat may see
// and change which job is decided by tools.CanAccessCronJob, the same rule
// the cron tool applies.
type cronCommands struct {
	cs  *cron.CronService
	cfg *config.Config
}

func newCronCommands(cs *cron.CronService, cfg *config.Config) *cronCommands {
	return &cronCommands{cs: cs, cfg: cfg}
}

func (c *cronCommands) canAccess(job *cron.CronJob, channel, chatID string) bool {
	var allowed []string
	if c.cfg != nil {
		allowed = c.cfg.Tools.Cron.CommandAllowedRemotes
	}
	return tools.CanAccessCronJob(job, channel, chatID, allowed)
}

func (c *cronCommands) List(channel, chatID string) []commands.CronJobView {
	var out []commands.CronJobView
	for _, job := range c.cs.ListJobs(true) {
		if c.canAccess(&job, channel, chatID) {
			out = append(out, jobView(&job))
		}
	}
	return out
}

func (c *cronCommands) Enable(id string, on bool) error {
	_, err := c.cs.EnableJob(id, on)
	return err
}

func (c *cronCommands) RunNow(id string) error {
	return c.cs.RunNow(id)
}

func (c *cronCommands) History(id string, n int) []commands.CronRunView {
	runs := c.cs.History(id)
	if n > 0 && len(runs) > n {
		runs = runs[:n]
	}
	out := make([]commands.CronRunView, len(runs))
	for i, r := range runs {
		out[i] = commands.CronRunView{
			Started:  time.UnixMilli(r.StartedAtMS),
			Duration: time.Duration(r.DurationMS) * time.Millisecond,
			Status:   r.Status,
			Error:    r.Error,
		}
	}
	return out
}

func (c *cronCommands) Templates() []commands.CronTemplateView {
	list := automation.List()
	out := make([]commands.CronTemplateView, len(list))
	for i, t := range list {
		v := commands.CronTemplateView{ID: t.ID, Title: t.Title}
		for _, p := range t.Params {
			if p.Required {
				v.Required = append(v.Required, p.Name)
			} else {
				v.Optional = append(v.Optional, p.Name)
			}
		}
		out[i] = v
	}
	return out
}

func (c *cronCommands) Create(
	tpl string,
	params map[string]string,
	channel, to string,
) (commands.CronJobView, error) {
	if _, ok := automation.Get(tpl); !ok {
		return commands.CronJobView{}, fmt.Errorf("unknown template %q; see /cron templates", tpl)
	}
	job, created, err := automation.CreateJob(c.cs, c.cfg, tpl, params, time.Now(), channel, to)
	if err != nil {
		return commands.CronJobView{}, err
	}
	v := jobView(job)
	v.Created = created
	return v, nil
}

func (c *cronCommands) Resolve(id, channel, chatID string) (string, error) {
	id = strings.TrimSpace(id)
	if len(id) < minJobIDPrefix {
		return "", fmt.Errorf("job id %q is too short: give at least %d characters", id, minJobIDPrefix)
	}
	var matches []string
	for _, job := range c.cs.ListJobs(true) {
		if !c.canAccess(&job, channel, chatID) || !strings.HasPrefix(job.ID, id) {
			continue
		}
		if job.ID == id {
			return id, nil
		}
		matches = append(matches, job.ID)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no job %q in this chat; see /cron list", id)
	case 1:
		return matches[0], nil
	}
	sort.Strings(matches)
	return "", errors.New("ambiguous job id " + id + ", matches: " + strings.Join(matches, ", "))
}

func jobView(job *cron.CronJob) commands.CronJobView {
	v := commands.CronJobView{
		ID:       job.ID,
		Name:     job.Name,
		Schedule: scheduleText(job.Schedule),
		State:    "on",
	}
	if !job.Enabled {
		v.State = "paused"
		if job.State.LastError != "" && cron.StripAutoDisabledPrefix(job.State.LastError) != job.State.LastError {
			v.State = "auto-disabled"
		}
	}
	if job.State.NextRunAtMS != nil {
		v.NextRun = time.UnixMilli(*job.State.NextRunAtMS)
	}
	return v
}

// scheduleText renders a schedule for a chat reply.
func scheduleText(s cron.CronSchedule) string {
	switch s.Kind {
	case "at":
		if s.AtMS != nil {
			return "once at " + time.UnixMilli(*s.AtMS).Local().Format("2006-01-02 15:04")
		}
	case "every":
		if s.EveryMS != nil {
			d := time.Duration(*s.EveryMS) * time.Millisecond
			switch {
			case d%time.Hour == 0:
				return fmt.Sprintf("every %dh", d/time.Hour)
			case d%time.Minute == 0:
				return fmt.Sprintf("every %dm", d/time.Minute)
			default:
				return "every " + d.String()
			}
		}
	case "cron":
		if s.TZ != "" {
			return fmt.Sprintf("cron %s (%s)", s.Expr, s.TZ)
		}
		return "cron " + s.Expr
	}
	return s.Kind
}
