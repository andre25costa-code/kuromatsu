package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/commands"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

func newTestCronService(t *testing.T) *cron.CronService {
	t.Helper()
	cs := cron.NewCronService(filepath.Join(t.TempDir(), "cron", "jobs.json"), nil)
	cs.SetMaxSleep(100 * time.Millisecond)
	return cs
}

func addMessageJob(t *testing.T, cs *cron.CronService, name, channel, to string) *cron.CronJob {
	t.Helper()
	every := int64(3_600_000)
	job, _, err := cs.AddJobWithPayload(name, cron.CronSchedule{Kind: "every", EveryMS: &every},
		cron.CronPayload{Message: name, Channel: channel, To: to})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestCronAdapter_RunNowAndHistory(t *testing.T) {
	cs := newTestCronService(t)
	var fail atomic.Bool
	var runs atomic.Int32
	cs.SetOnJob(func(*cron.CronJob) (string, error) {
		runs.Add(1)
		if fail.Load() {
			return "", errors.New("provider down")
		}
		return "ok", nil
	})
	cfg := config.DefaultConfig()
	api := newCronCommands(cs, cfg)
	job := addMessageJob(t, cs, "daily", "telegram", "42")

	if err := cs.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cs.Stop)

	waitRuns := func(n int) []commands.CronRunView {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			got := api.History(job.ID, 5)
			if len(got) >= n {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d run(s) recorded, want %d", len(got), n)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	if err := api.RunNow(job.ID); err != nil {
		t.Fatal(err)
	}
	got := waitRuns(1)
	if got[0].Status != cron.RunStatusOK || got[0].Started.IsZero() {
		t.Fatalf("first run = %+v", got[0])
	}

	fail.Store(true)
	if err := api.RunNow(job.ID); err != nil {
		t.Fatal(err)
	}
	got = waitRuns(2)
	if got[0].Status != cron.RunStatusError || !strings.Contains(got[0].Error, "provider down") {
		t.Fatalf("latest run = %+v, want the failure first", got[0])
	}

	// Pause and resume: resume starts a new failure streak.
	if j, _ := cs.GetJob(job.ID); j.State.ConsecutiveFailures != 1 {
		t.Fatalf("failures = %d, want 1", j.State.ConsecutiveFailures)
	}
	if err := api.Enable(job.ID, false); err != nil {
		t.Fatal(err)
	}
	if j, _ := cs.GetJob(job.ID); j.Enabled {
		t.Fatal("pause left the job on")
	}
	if v := api.List("cli", "direct"); len(v) != 1 || v[0].State != "paused" {
		t.Fatalf("list after pause = %+v", v)
	}
	if err := api.RunNow(job.ID); !errors.Is(err, cron.ErrJobDisabled) {
		t.Fatalf("RunNow on a paused job = %v", err)
	}
	if err := api.Enable(job.ID, true); err != nil {
		t.Fatal(err)
	}
	if j, _ := cs.GetJob(job.ID); !j.Enabled || j.State.ConsecutiveFailures != 0 {
		t.Fatalf("after resume: enabled=%v failures=%d", j.Enabled, j.State.ConsecutiveFailures)
	}

	// History caps at n runs.
	if h := api.History(job.ID, 1); len(h) != 1 {
		t.Fatalf("History(id, 1) returned %d runs", len(h))
	}
}

func TestCronAdapter_AccessRule(t *testing.T) {
	cs := newTestCronService(t)
	cfg := config.DefaultConfig()
	api := newCronCommands(cs, cfg)
	mine := addMessageJob(t, cs, "mine", "telegram", "42")
	other := addMessageJob(t, cs, "other", "telegram", "77")
	cmdJob := addMessageJob(t, cs, "check", "telegram", "42")
	cmdJob.Payload.Command = "df -h"
	if err := cs.UpdateJob(cmdJob); err != nil {
		t.Fatal(err)
	}

	ids := func(views []commands.CronJobView) string {
		s := make([]string, 0, len(views))
		for _, v := range views {
			s = append(s, v.Name)
		}
		return strings.Join(s, ",")
	}
	if got := ids(api.List("telegram", "42")); got != "mine" {
		t.Fatalf("telegram/42 sees %q, want only its message job", got)
	}
	if got := ids(api.List("cli", "direct")); got != "mine,other,check" {
		t.Fatalf("internal channel sees %q, want every job", got)
	}
	if got := ids(api.List("telegram", "")); got != "" {
		t.Fatalf("no chat id sees %q", got)
	}
	if _, err := api.Resolve(other.ID[:6], "telegram", "42"); err == nil {
		t.Fatal("another chat's job resolved")
	}
	if _, err := api.Resolve(cmdJob.ID, "telegram", "42"); err == nil {
		t.Fatal("a command job resolved from a chat outside command_allowed_remotes")
	}
	if id, err := api.Resolve(mine.ID[:4], "telegram", "42"); err != nil || id != mine.ID {
		t.Fatalf("Resolve(prefix) = %q, %v", id, err)
	}
	if _, err := api.Resolve(mine.ID[:3], "telegram", "42"); err == nil {
		t.Fatal("a 3-character prefix resolved")
	}

	cfg.Tools.Cron.CommandAllowedRemotes = []string{"telegram:42"}
	if got := ids(api.List("telegram", "42")); got != "mine,check" {
		t.Fatalf("with the chat allowed for commands, telegram/42 sees %q", got)
	}
}

func TestCronAdapter_CreateFromTemplate(t *testing.T) {
	cs := newTestCronService(t)
	cfg := config.DefaultConfig()
	cfg.Tools.Cron.Enabled = true
	cfg.Tools.Web.Enabled = true
	cfg.Tools.WebFetch.Enabled = true
	api := newCronCommands(cs, cfg)

	when := time.Now().Add(2 * time.Minute).Format(time.RFC3339)
	view, err := api.Create("lembrete", map[string]string{"texto": "beber água", "quando": when}, "telegram", "42")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Created || view.NextRun.IsZero() {
		t.Fatalf("view = %+v", view)
	}
	job, ok := cs.GetJob(view.ID)
	if !ok {
		t.Fatal("job not stored")
	}
	if job.Schedule.Kind != "at" || !job.DeleteAfterRun || job.Payload.Channel != "telegram" ||
		job.Payload.To != "42" ||
		job.Payload.Message != "Envie ao usuário exatamente este lembrete, sem comentários: beber água" {
		t.Fatalf("job = %+v", job)
	}

	// The same request again returns the existing job.
	again, err := api.Create("lembrete", map[string]string{"texto": "beber água", "quando": when}, "telegram", "42")
	if err != nil || again.Created || again.ID != view.ID {
		t.Fatalf("repeat = %+v, %v", again, err)
	}

	cfg.Tools.Web.Enabled = false
	_, err = api.Create(
		"monitorar-pagina",
		map[string]string{"url": "https://example.org", "condicao": "c"},
		"telegram",
		"42",
	)
	if err == nil || !strings.Contains(err.Error(), "tools.web.enabled") {
		t.Fatalf("Create with web off = %v, want the missing requirement", err)
	}
	if _, err = api.Create("email", nil, "telegram", "42"); err == nil {
		t.Fatal("unknown template accepted")
	}

	// The daily schedule carries the configured zone, or none.
	cfg.Timezone = ""
	view, err = api.Create("resumo-diario", nil, "telegram", "42")
	if err != nil {
		t.Fatal(err)
	}
	if job, _ := cs.GetJob(view.ID); job.Schedule.TZ != "" || job.Schedule.Expr != "0 8 * * *" {
		t.Fatalf("schedule = %+v", job.Schedule)
	}
}

func TestBuildCommandsRuntime_CronFollowsSetCronService(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &turnProfileCaptureProvider{})
	rt := al.buildCommandsRuntime(context.Background(), al.registry.GetDefaultAgent(), &processOptions{})
	if rt.Cron == nil || rt.Cron() != nil {
		t.Fatal("without a scheduler Cron must be set and return nil")
	}
	al.SetCronService(newTestCronService(t))
	if rt.Cron() == nil {
		t.Fatal("Cron returned nil after SetCronService")
	}
	if rt.CanMutate == nil || !rt.CanMutate("cli", "x") || rt.CanMutate("telegram", "x") {
		t.Fatal("CanMutate does not follow the owner rule")
	}
	if rt.ConfigSummary == nil {
		t.Fatal("ConfigSummary not set")
	}
	if s, _ := rt.ConfigSummary(); !strings.HasPrefix(s, "Model: ") {
		t.Fatalf("summary = %q", s)
	}
}

func TestCronAdapter_AmbiguousPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	every := int64(3_600_000)
	store := cron.CronStore{Version: 1}
	for _, id := range []string{"abcd1111", "abcd2222", "ffff0000"} {
		store.Jobs = append(store.Jobs, cron.CronJob{
			ID: id, Name: id, Enabled: true,
			Schedule: cron.CronSchedule{Kind: "every", EveryMS: &every},
			Payload:  cron.CronPayload{Kind: "agent_turn", Message: id, Channel: "cli", To: "direct"},
		})
	}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	api := newCronCommands(cron.NewCronService(path, nil), config.DefaultConfig())

	_, err = api.Resolve("abcd", "cli", "direct")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") ||
		!strings.Contains(err.Error(), "abcd1111") || !strings.Contains(err.Error(), "abcd2222") {
		t.Fatalf("Resolve(abcd) = %v, want an ambiguity error listing both ids", err)
	}
	if id, err := api.Resolve("abcd2", "cli", "direct"); err != nil || id != "abcd2222" {
		t.Fatalf("Resolve(abcd2) = %q, %v", id, err)
	}
	if id, err := api.Resolve("ffff0000", "cli", "direct"); err != nil || id != "ffff0000" {
		t.Fatalf("exact id = %q, %v", id, err)
	}
}
