package webui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/automation"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

// The UI user is the owner, so jobs are not filtered by chat the way the
// cron tool and /cron filter them.

type jobView struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Enabled             bool              `json:"enabled"`
	State               string            `json:"state"`
	Schedule            cron.CronSchedule `json:"schedule"`
	Message             string            `json:"message"`
	Channel             string            `json:"channel"`
	To                  string            `json:"to"`
	NextRunMS           *int64            `json:"next_run_ms,omitempty"`
	LastStatus          string            `json:"last_status,omitempty"`
	LastError           string            `json:"last_error,omitempty"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
}

func viewJob(j *cron.CronJob) jobView {
	state := "on"
	if !j.Enabled {
		state = "paused"
		if j.State.LastError != "" && cron.StripAutoDisabledPrefix(j.State.LastError) != j.State.LastError {
			state = "auto-disabled"
		}
	}
	return jobView{
		ID: j.ID, Name: j.Name, Enabled: j.Enabled, State: state, Schedule: j.Schedule,
		Message: j.Payload.Message, Channel: j.Payload.Channel, To: j.Payload.To,
		NextRunMS: j.State.NextRunAtMS, LastStatus: j.State.LastStatus, LastError: j.State.LastError,
		ConsecutiveFailures: j.State.ConsecutiveFailures,
	}
}

// scheduler returns the current scheduler or answers 503 while a reload
// replaces it.
func (s *server) scheduler(w http.ResponseWriter) (*cron.CronService, bool) {
	var cs *cron.CronService
	if s.d.Cron != nil {
		cs = s.d.Cron()
	}
	if cs == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler not running (reloading?)")
		return nil, false
	}
	return cs, true
}

func (s *server) listCron(w http.ResponseWriter, _ *http.Request) {
	cs, ok := s.scheduler(w)
	if !ok {
		return
	}
	jobs := cs.ListJobs(true)
	out := make([]jobView, 0, len(jobs))
	for i := range jobs {
		out = append(out, viewJob(&jobs[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

// defaultTarget is where a new automation reports by default: the first
// enabled channel (name order) with an allow_from entry other than "*",
// and that entry, reduced to the ID when it has one. Without such a
// channel both are empty, so the user has to choose: a job without a
// channel and a recipient is refused.
func defaultTarget(cfg *config.Config) (channel, to string) {
	for _, c := range channelViews(cfg) {
		if !c.Enabled {
			continue
		}
		for _, e := range c.AllowFrom {
			if e == "*" {
				continue
			}
			if id, ok := setup.OwnerID(e, c.Type); ok {
				return c.Name, id
			}
			return c.Name, e
		}
	}
	return "", ""
}

func (s *server) cronTemplates(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.runtimeConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	type tplView struct {
		automation.Template
		Unmet []string `json:"unmet"`
	}
	list := automation.List()
	out := make([]tplView, 0, len(list))
	for _, t := range list {
		out = append(out, tplView{Template: t, Unmet: automation.Unmet(t, cfg)})
	}
	ch, to := defaultTarget(cfg)
	writeJSON(w, http.StatusOK, map[string]any{
		"templates": out, "default_target": map[string]string{"channel": ch, "to": to},
	})
}

func (s *server) createCron(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Template string            `json:"template"`
		Params   map[string]string `json:"params"`
		Channel  string            `json:"channel"`
		To       string            `json:"to"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cs, ok := s.scheduler(w)
	if !ok {
		return
	}
	cfg, err := s.runtimeConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	job, created, err := automation.CreateJob(cs, cfg, body.Template, body.Params, s.d.Now(),
		strings.TrimSpace(body.Channel), strings.TrimSpace(body.To))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, automation.ErrInvalid) {
			status = http.StatusUnprocessableEntity
		}
		writeError(w, status, err.Error())
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"job": viewJob(job), "created": created})
}

// editCron changes a job's name, message and schedule; other fields stay.
func (s *server) editCron(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     *string            `json:"name"`
		Message  *string            `json:"message"`
		Schedule *cron.CronSchedule `json:"schedule"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cs, ok := s.scheduler(w)
	if !ok {
		return
	}
	job, found := cs.GetJob(r.PathValue("id"))
	if !found {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if body.Name != nil {
		if strings.TrimSpace(*body.Name) == "" {
			writeError(w, http.StatusUnprocessableEntity, "name cannot be empty")
			return
		}
		job.Name = strings.TrimSpace(*body.Name)
	}
	if body.Message != nil {
		job.Payload.Message = *body.Message
	}
	if body.Schedule != nil {
		job.Schedule = *body.Schedule
	}
	if err := cs.UpdateJob(job); err != nil {
		s.cronError(w, err)
		return
	}
	if updated, ok := cs.GetJob(job.ID); ok {
		job = updated
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": viewJob(job)})
}

func (s *server) cronError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cron.ErrJobNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, cron.ErrJobDisabled):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	}
}

func (s *server) setCronEnabled(w http.ResponseWriter, r *http.Request, on bool) {
	cs, ok := s.scheduler(w)
	if !ok {
		return
	}
	job, err := cs.EnableJob(r.PathValue("id"), on)
	if err != nil {
		s.cronError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": viewJob(job)})
}

func (s *server) pauseCron(w http.ResponseWriter, r *http.Request)  { s.setCronEnabled(w, r, false) }
func (s *server) resumeCron(w http.ResponseWriter, r *http.Request) { s.setCronEnabled(w, r, true) }

func (s *server) runCron(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.scheduler(w)
	if !ok {
		return
	}
	if err := cs.RunNow(r.PathValue("id")); err != nil {
		s.cronError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *server) cronHistory(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.scheduler(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, found := cs.GetJob(id); !found {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	runs := cs.History(id)
	if runs == nil {
		runs = []cron.CronRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *server) deleteCron(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.scheduler(w)
	if !ok {
		return
	}
	removed, err := cs.RemoveJob(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
