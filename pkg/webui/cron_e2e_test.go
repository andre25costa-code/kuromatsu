package webui

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

// startCron runs a real scheduler in a temp dir whose jobs go to the
// returned channel.
func startCron(t *testing.T, e *testEnv) <-chan cron.CronJob {
	t.Helper()
	fired := make(chan cron.CronJob, 8)
	cs := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	cs.SetOnJob(func(job *cron.CronJob) (string, error) {
		fired <- *job
		return "ok", nil
	})
	require.NoError(t, cs.Start())
	t.Cleanup(cs.Stop)
	e.cron.Store(cs)
	return fired
}

func decode(t *testing.T, body []byte, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(body, v), string(body))
}

func TestWebUI_CreateAutomationRunsThroughCron(t *testing.T) {
	e := newTestEnv(t)
	e.now = time.Now()
	fired := startCron(t, e)
	cookie, csrf := e.login()
	post := func(path, body string) *http.Request {
		r := req(http.MethodPost, path, body)
		r.AddCookie(cookie)
		r.Header.Set("Origin", testOrigin)
		r.Header.Set(csrfHeader, csrf)
		return r
	}

	// One-shot reminder a moment from now: the scheduler fires it by itself.
	when := time.Now().Add(2 * time.Second).Format(time.RFC3339)
	rec := e.do(post(
		"/api/ui/cron",
		`{"template":"lembrete","params":{"texto":"beber água","quando":"`+when+`"},"channel":"telegram","to":"987654"}`,
	))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	select {
	case job := <-fired:
		assert.Contains(t, job.Payload.Message, "beber água")
		assert.Equal(t, "telegram", job.Payload.Channel)
		assert.Equal(t, "987654", job.Payload.To)
	case <-time.After(5 * time.Second):
		t.Fatal("the reminder did not fire")
	}

	// A daily reminder run on demand keeps its job, so its history shows.
	rec = e.do(post("/api/ui/cron",
		`{"template":"lembrete","params":{"texto":"alongar","todo_dia":"07:00"},"channel":"telegram","to":"987654"}`))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		Job jobView `json:"job"`
	}
	decode(t, rec.Body.Bytes(), &created)

	rec = e.do(post("/api/ui/cron/"+created.Job.ID+"/run", ""))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	select {
	case job := <-fired:
		assert.Equal(t, created.Job.ID, job.ID)
		assert.Contains(t, job.Payload.Message, "alongar")
	case <-time.After(3 * time.Second):
		t.Fatal("run-now did not reach the executor within 3 s")
	}

	var hist struct {
		Runs []cron.CronRun `json:"runs"`
	}
	require.Eventually(t, func() bool {
		r := req(http.MethodGet, "/api/ui/cron/"+created.Job.ID+"/history", "")
		r.AddCookie(cookie)
		hr := e.do(r)
		if hr.Code != http.StatusOK {
			return false
		}
		decode(t, hr.Body.Bytes(), &hist)
		return len(hist.Runs) == 1
	}, 3*time.Second, 50*time.Millisecond)
	assert.Equal(t, "ok", hist.Runs[0].Status)

	// Pause, edit, resume, delete.
	rec = e.do(post("/api/ui/cron/"+created.Job.ID+"/pause", ""))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, http.StatusConflict, e.do(post("/api/ui/cron/"+created.Job.ID+"/run", "")).Code)
	patch := req(http.MethodPatch, "/api/ui/cron/"+created.Job.ID, `{"name":"Alongar","message":"alongar 5 min"}`)
	patch.AddCookie(cookie)
	patch.Header.Set("Origin", testOrigin)
	patch.Header.Set(csrfHeader, csrf)
	rec = e.do(patch)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	job, ok := e.cron.Load().GetJob(created.Job.ID)
	require.True(t, ok)
	assert.Equal(t, "Alongar", job.Name)
	assert.Equal(t, "alongar 5 min", job.Payload.Message)
	assert.False(t, job.Enabled)
	require.Equal(t, http.StatusOK, e.do(post("/api/ui/cron/"+created.Job.ID+"/resume", "")).Code)

	del := req(http.MethodDelete, "/api/ui/cron/"+created.Job.ID, "")
	del.AddCookie(cookie)
	del.Header.Set("Origin", testOrigin)
	del.Header.Set(csrfHeader, csrf)
	require.Equal(t, http.StatusOK, e.do(del).Code)
	assert.Equal(t, http.StatusNotFound, e.do(post("/api/ui/cron/"+created.Job.ID+"/run", "")).Code)
}

func TestWebUI_CronValidationAndTemplates(t *testing.T) {
	e := newTestEnv(t)
	e.now = time.Now()
	startCron(t, e)

	rec := e.do(bearer(req(http.MethodGet, "/api/ui/cron/templates", "")))
	require.Equal(t, http.StatusOK, rec.Code)
	var tpl struct {
		Templates []struct {
			ID    string   `json:"id"`
			Unmet []string `json:"unmet"`
		} `json:"templates"`
		DefaultTarget map[string]string `json:"default_target"`
	}
	decode(t, rec.Body.Bytes(), &tpl)
	assert.NotEmpty(t, tpl.Templates)
	assert.Equal(t, map[string]string{"channel": "telegram", "to": "987654"}, tpl.DefaultTarget)

	for _, body := range []string{
		`{"template":"nope","params":{}}`,
		`{"template":"lembrete","params":{"texto":"x"}}`,
		`{"template":"lembrete","params":{"texto":"x","quando":"2001-01-01T00:00:00Z"}}`,
	} {
		rec = e.do(bearer(req(http.MethodPost, "/api/ui/cron", body)))
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, body)
	}
}

func TestWebUI_CronGetterAfterReload(t *testing.T) {
	e := newTestEnv(t)
	assert.Equal(t, http.StatusServiceUnavailable, e.do(bearer(req(http.MethodGet, "/api/ui/cron", ""))).Code)

	first := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	_, err := first.AddJob("old job", cron.CronSchedule{Kind: "cron", Expr: "0 7 * * *"}, "m", "", "")
	require.NoError(t, err)
	e.cron.Store(first)
	rec := e.do(bearer(req(http.MethodGet, "/api/ui/cron", "")))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "old job")

	// A reload builds a new scheduler; the next call must use it.
	second := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	_, err = second.AddJob("new job", cron.CronSchedule{Kind: "cron", Expr: "0 8 * * *"}, "m", "", "")
	require.NoError(t, err)
	e.cron.Store(second)
	rec = e.do(bearer(req(http.MethodGet, "/api/ui/cron", "")))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "new job")
	assert.NotContains(t, rec.Body.String(), "old job")
}

func TestDefaultTarget_SkipsChannelsWithoutRecipient(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels = config.ChannelsConfig{
		"pico":     {Enabled: true, Type: "pico", AllowFrom: config.FlexibleStringSlice{"*"}},
		"telegram": {Enabled: true, Type: "telegram", AllowFrom: config.FlexibleStringSlice{"*", "telegram:42|ana"}},
		"aaa":      {Enabled: false, Type: "telegram", AllowFrom: config.FlexibleStringSlice{"7"}},
	}
	ch, to := defaultTarget(cfg)
	assert.Equal(t, "telegram", ch)
	assert.Equal(t, "42", to)

	cfg.Channels = config.ChannelsConfig{
		"pico": {Enabled: true, Type: "pico", AllowFrom: config.FlexibleStringSlice{"*"}},
	}
	// No recipient to suggest: no channel either, the user must choose.
	ch, to = defaultTarget(cfg)
	assert.Empty(t, ch)
	assert.Empty(t, to)

	cfg.Channels = config.ChannelsConfig{}
	ch, to = defaultTarget(cfg)
	assert.Empty(t, ch)
	assert.Empty(t, to)
}

// The web UI must not create a template job that would run as an internal
// channel, report nowhere, or report to a channel the config does not run.
func TestWebUI_CreateCronRefusesUnsafeTarget(t *testing.T) {
	e := newTestEnv(t)
	e.now = time.Now()
	startCron(t, e)

	params := `"template":"lembrete","params":{"texto":"x","todo_dia":"07:00"}`
	for _, target := range []string{
		``,
		`,"channel":"","to":"987654"`,
		`,"channel":"cli","to":"987654"`,
		`,"channel":"system","to":"987654"`,
		`,"channel":"subagent","to":"987654"`,
		`,"channel":"nope","to":"987654"`,
		`,"channel":"pico","to":"987654"`, // configured but disabled
		`,"channel":"telegram","to":""`,
	} {
		body := `{` + params + target + `}`
		rec := e.do(bearer(req(http.MethodPost, "/api/ui/cron", body)))
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, body)
		assert.Contains(t, rec.Body.String(), "channel", body)
	}
	assert.Empty(t, e.cron.Load().ListJobs(true))
}
