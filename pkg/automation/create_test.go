package automation

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func TestCreateJob(t *testing.T) {
	cs := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	cfg := config.DefaultConfig()
	cfg.Tools.Cron.Enabled = true
	cfg.Timezone = "America/Sao_Paulo"
	cfg.Channels["telegram"].Enabled = true
	now := time.Now()

	job, created, err := CreateJob(cs, cfg, "lembrete",
		map[string]string{"texto": "beber água", "todo_dia": "08:30"}, now, "telegram", "123")
	if err != nil || !created {
		t.Fatalf("CreateJob: created=%v err=%v", created, err)
	}
	if job.Schedule.Kind != "cron" || job.Schedule.TZ != "America/Sao_Paulo" {
		t.Fatalf("schedule = %+v", job.Schedule)
	}
	if job.Payload.Channel != "telegram" || job.Payload.To != "123" {
		t.Fatalf("payload destination = %q/%q", job.Payload.Channel, job.Payload.To)
	}

	_, created, err = CreateJob(cs, cfg, "lembrete",
		map[string]string{"texto": "beber água", "todo_dia": "08:30"}, now, "telegram", "123")
	if err != nil || created {
		t.Fatalf("identical job: created=%v err=%v, want existing job", created, err)
	}

	for name, call := range map[string]func() error{
		"unknown template": func() error {
			_, _, e := CreateJob(cs, cfg, "nope", nil, now, "", "")
			return e
		},
		"bad param": func() error {
			_, _, e := CreateJob(cs, cfg, "lembrete", map[string]string{"texto": "x"}, now, "", "")
			return e
		},
		"unmet requirement": func() error {
			off := config.DefaultConfig()
			off.Tools.Cron.Enabled = false
			_, _, e := CreateJob(cs, off, "lembrete", map[string]string{"texto": "x", "todo_dia": "08:00"}, now, "", "")
			return e
		},
	} {
		err := call()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
		if err != nil && err.Error() == ErrInvalid.Error() {
			t.Errorf("%s: message lost its cause", name)
		}
	}

	if _, _, err := CreateJob(nil, cfg, "lembrete", nil, now, "", ""); err == nil || errors.Is(err, ErrInvalid) {
		t.Fatalf("nil scheduler: err = %v, want a non-input error", err)
	}
}

// A template job's turn runs with its channel's trust, so it must never
// get an empty or internal channel (which would run it as "cli"), a
// channel the config does not enable, or no recipient.
func TestCreateJob_RefusesUnsafeTarget(t *testing.T) {
	cs := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	cfg := config.DefaultConfig()
	cfg.Tools.Cron.Enabled = true
	cfg.Channels["telegram"].Enabled = true
	cfg.Channels["off"] = &config.Channel{Type: "telegram", Enabled: false}
	params := map[string]string{"texto": "beber água", "todo_dia": "08:30"}
	now := time.Now()

	for _, tc := range []struct{ name, channel, to string }{
		{"empty channel", "", "123"},
		{"blank channel", "   ", "123"},
		{"cli", "cli", "123"},
		{"system", "system", "123"},
		{"subagent", "subagent", "123"},
		{"unknown channel", "matrix", "123"},
		{"disabled channel", "off", "123"},
		{"empty recipient", "telegram", ""},
		{"blank recipient", "telegram", "  "},
	} {
		_, _, err := CreateJob(cs, cfg, "lembrete", params, now, tc.channel, tc.to)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
	if err := checkTarget(nil, "telegram", "123"); !errors.Is(err, ErrInvalid) {
		t.Errorf("nil config: err = %v, want ErrInvalid", err)
	}
	if n := len(cs.ListJobs(true)); n != 0 {
		t.Fatalf("refused targets stored %d job(s)", n)
	}

	job, created, err := CreateJob(cs, cfg, "lembrete", params, now, " telegram ", " 123 ")
	if err != nil || !created {
		t.Fatalf("valid target: created=%v err=%v", created, err)
	}
	if job.Payload.Channel != "telegram" || job.Payload.To != "123" {
		t.Fatalf("payload destination = %q/%q, want trimmed telegram/123", job.Payload.Channel, job.Payload.To)
	}
}
