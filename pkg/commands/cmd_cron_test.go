package commands

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/constants"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

type fakeJob struct {
	view            CronJobView
	channel, chatID string
}

// fakeCron is a CronAPI over an in-memory job list, applying the same
// visibility rule as the real adapter: internal channels see every job,
// others only the jobs delivered to their own chat.
type fakeCron struct {
	jobs    []*fakeJob
	enabled map[string]bool
	ran     []string
	created []struct {
		tpl             string
		params          map[string]string
		channel, chatID string
	}
}

func (f *fakeCron) visible(j *fakeJob, channel, chatID string) bool {
	return constants.IsInternalChannel(channel) || (j.channel == channel && j.chatID == chatID)
}

func (f *fakeCron) List(channel, chatID string) []CronJobView {
	var out []CronJobView
	for _, j := range f.jobs {
		if f.visible(j, channel, chatID) {
			out = append(out, j.view)
		}
	}
	return out
}

func (f *fakeCron) Enable(id string, on bool) error {
	if f.enabled == nil {
		f.enabled = map[string]bool{}
	}
	f.enabled[id] = on
	return nil
}

func (f *fakeCron) RunNow(id string) error { f.ran = append(f.ran, id); return nil }

func (f *fakeCron) History(string, int) []CronRunView { return nil }

func (f *fakeCron) Templates() []CronTemplateView {
	return []CronTemplateView{{ID: "lembrete", Title: "Lembrete", Required: []string{"texto"}}}
}

func (f *fakeCron) Create(tpl string, params map[string]string, channel, to string) (CronJobView, error) {
	f.created = append(f.created, struct {
		tpl             string
		params          map[string]string
		channel, chatID string
	}{tpl, params, channel, to})
	return CronJobView{ID: "newjob0123456789", Name: "Lembrete", Schedule: "once", Created: true}, nil
}

func (f *fakeCron) Resolve(id, channel, chatID string) (string, error) {
	if len(id) < 4 {
		return "", errors.New("job id needs at least 4 characters")
	}
	var matches []string
	for _, j := range f.jobs {
		if f.visible(j, channel, chatID) && strings.HasPrefix(j.view.ID, id) {
			if j.view.ID == id {
				return id, nil
			}
			matches = append(matches, j.view.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no job %q here", id)
	case 1:
		return matches[0], nil
	}
	sort.Strings(matches)
	return "", fmt.Errorf("ambiguous id %q: %s", id, strings.Join(matches, ", "))
}

func newFakeCron() *fakeCron {
	return &fakeCron{jobs: []*fakeJob{
		{
			view:    CronJobView{ID: "aaaa1111bbbb", Name: "mine", Schedule: "every 1h", State: "on"},
			channel: "telegram",
			chatID:  "42",
		},
		{
			view:    CronJobView{ID: "aaaa2222cccc", Name: "mine too", Schedule: "every 2h", State: "on"},
			channel: "telegram",
			chatID:  "42",
		},
		{
			view:    CronJobView{ID: "dddd3333eeee", Name: "someone else", Schedule: "every 3h", State: "on"},
			channel: "telegram",
			chatID:  "77",
		},
	}}
}

func cronRuntime(t *testing.T, api CronAPI, allowFrom ...string) *Runtime {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Channels = config.ChannelsConfig{config.ChannelTelegram: telegramChannel(t, allowFrom...)}
	return &Runtime{
		Config:    cfg,
		Cron:      func() CronAPI { return api },
		CanMutate: func(channel, senderID string) bool { return setup.CanMutate(cfg, channel, senderID) },
	}
}

func TestCronCommand_ListScopedToChat(t *testing.T) {
	api := newFakeCron()
	rt := cronRuntime(t, api, "123")

	reply := runCommand(t, rt, "telegram", "42", "123", "/cron list")
	if !strings.Contains(reply, "aaaa1111") || !strings.Contains(reply, "aaaa2222") ||
		strings.Contains(reply, "dddd3333") {
		t.Fatalf("external chat list = %q, want only its own jobs", reply)
	}
	if strings.Contains(reply, "aaaa1111bbbb") {
		t.Fatalf("list shows full ids, want 8 characters: %q", reply)
	}
	reply = runCommand(t, rt, "cli", "direct", "local", "/cron list")
	for _, id := range []string{"aaaa1111", "aaaa2222", "dddd3333"} {
		if !strings.Contains(reply, id) {
			t.Fatalf("internal channel list = %q, want every job", reply)
		}
	}
	// Reading needs no owner rights.
	reply = runCommand(t, rt, "telegram", "77", "999", "/cron list")
	if !strings.Contains(reply, "dddd3333") || strings.Contains(reply, "aaaa") {
		t.Fatalf("other chat list = %q", reply)
	}
}

func TestCronCommand_PauseResumePrefix(t *testing.T) {
	api := newFakeCron()
	rt := cronRuntime(t, api, "123")

	reply := runCommand(t, rt, "telegram", "42", "123", "/cron pause aaaa1")
	if on, ok := api.enabled["aaaa1111bbbb"]; !ok || on {
		t.Fatalf("pause by prefix: enabled=%v reply=%q", api.enabled, reply)
	}
	runCommand(t, rt, "telegram", "42", "123", "/cron resume aaaa1111")
	if !api.enabled["aaaa1111bbbb"] {
		t.Fatalf("resume: enabled=%v", api.enabled)
	}

	reply = runCommand(t, rt, "telegram", "42", "123", "/cron pause aaaa")
	if !strings.Contains(reply, "ambiguous") || !strings.Contains(reply, "aaaa1111bbbb") ||
		!strings.Contains(reply, "aaaa2222cccc") {
		t.Fatalf("ambiguous prefix reply = %q, want the candidates", reply)
	}
	reply = runCommand(t, rt, "telegram", "42", "123", "/cron pause dddd3333")
	if _, touched := api.enabled["dddd3333eeee"]; touched || !strings.Contains(reply, "no job") {
		t.Fatalf("another chat's job changed or wrong reply: %q", reply)
	}
	reply = runCommand(t, rt, "telegram", "42", "123", "/cron run aaaa2222")
	if len(api.ran) != 1 || api.ran[0] != "aaaa2222cccc" || !strings.Contains(reply, "queued") {
		t.Fatalf("run: ran=%v reply=%q", api.ran, reply)
	}

	// Not the owner: nothing changes.
	before := len(api.enabled)
	for _, cmd := range []string{"/cron pause aaaa1111", "/cron resume aaaa1111", "/cron run aaaa1111", "/cron new lembrete texto=x"} {
		if reply := runCommand(t, rt, "telegram", "42", "456", cmd); reply != ownerOnlyMsg {
			t.Fatalf("%s by a non-owner: reply = %q", cmd, reply)
		}
	}
	if len(api.enabled) != before || len(api.ran) != 1 || len(api.created) != 0 {
		t.Fatal("a non-owner changed jobs")
	}
}

func TestCronCommand_NewParsesQuotedParams(t *testing.T) {
	api := newFakeCron()
	rt := cronRuntime(t, api, "123")
	reply := runCommand(t, rt, "telegram", "42", "123",
		`/cron new lembrete texto="beber água" quando=2026-10-15T14:00:00-03:00`)
	if len(api.created) != 1 {
		t.Fatalf("created = %d, reply = %q", len(api.created), reply)
	}
	c := api.created[0]
	if c.tpl != "lembrete" || c.params["texto"] != "beber água" || c.params["quando"] != "2026-10-15T14:00:00-03:00" ||
		c.channel != "telegram" || c.chatID != "42" || len(c.params) != 2 {
		t.Fatalf("Create got %+v", c)
	}
	if !strings.Contains(reply, "newjob01") || !strings.Contains(reply, "created") {
		t.Fatalf("reply = %q", reply)
	}
	if reply := runCommand(
		t,
		rt,
		"telegram",
		"42",
		"123",
		"/cron new lembrete texto",
	); !strings.Contains(
		reply,
		"key=value",
	) {
		t.Fatalf("bad parameter reply = %q", reply)
	}
	if reply := runCommand(t, rt, "telegram", "42", "123", "/cron templates"); !strings.Contains(reply, "lembrete") ||
		!strings.Contains(reply, "texto") {
		t.Fatalf("templates reply = %q", reply)
	}
}

func TestCronCommand_UnavailableWithoutGateway(t *testing.T) {
	// The agent loop always sets Cron; outside the gateway it returns nil.
	rt := &Runtime{Cron: func() CronAPI { return nil }, CanMutate: func(string, string) bool { return true }}
	for _, cmd := range []string{
		"/cron list", "/cron pause abcd", "/cron run abcd", "/cron history abcd",
		"/cron templates", "/cron new lembrete texto=x",
	} {
		if reply := runCommand(t, rt, "cli", "direct", "local", cmd); reply != unavailableMsg {
			t.Fatalf("%s: reply = %q", cmd, reply)
		}
	}
	if reply := runCommand(t, &Runtime{}, "cli", "direct", "local", "/cron list"); reply != unavailableMsg {
		t.Fatalf("nil Cron: reply = %q", reply)
	}
}

func TestCronCommand_HistoryFormat(t *testing.T) {
	api := &historyCron{fakeCron: newFakeCron()}
	rt := cronRuntime(t, api, "123")
	reply := runCommand(t, rt, "telegram", "42", "123", "/cron history aaaa1111")
	if !strings.Contains(reply, "error: ") || !strings.Contains(reply, "1.5s") || !strings.Contains(reply, "ok") {
		t.Fatalf("history reply = %q", reply)
	}
	if strings.Contains(reply, strings.Repeat("x", 121)) || !strings.Contains(reply, strings.Repeat("x", 120)+"…") {
		t.Fatalf("error not truncated at 120 characters: %q", reply)
	}
}

type historyCron struct{ *fakeCron }

func (h *historyCron) History(id string, n int) []CronRunView {
	if n != 5 {
		panic("history asked for other than 5 runs")
	}
	start := time.Date(2026, 10, 15, 8, 0, 0, 0, time.Local)
	return []CronRunView{
		{Started: start, Duration: 1500 * time.Millisecond, Status: "error", Error: strings.Repeat("x", 300)},
		{Started: start.Add(-time.Hour), Duration: time.Second, Status: "ok"},
	}
}
