package commands

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	cronHistoryRuns   = 5
	cronHistoryErrMax = 120
	cronShortID       = 8
)

// cronCommand implements /cron: list, pause, resume, run and history of the
// jobs this chat may see, the automation template catalog, and creating a
// job from a template. pause, resume, run and new need CanMutate.
func cronCommand() Definition {
	return Definition{
		Name:        "cron",
		Description: "List, pause, resume, run or create scheduled jobs",
		SubCommands: []SubCommand{
			{Name: "list", Description: "Jobs of this chat", Handler: cronList},
			{Name: "pause", Description: "Pause a job", ArgsUsage: "<id>", Handler: cronEnable(false)},
			{Name: "resume", Description: "Resume a job", ArgsUsage: "<id>", Handler: cronEnable(true)},
			{Name: "run", Description: "Run a job now", ArgsUsage: "<id>", Handler: cronRun},
			{Name: "history", Description: "Last runs of a job", ArgsUsage: "<id>", Handler: cronHistory},
			{Name: "templates", Description: "Automation templates", Handler: cronTemplates},
			{
				Name:        "new",
				Description: "Create a job from a template",
				ArgsUsage:   "<template> key=value...",
				Handler:     cronNew,
			},
		},
	}
}

// cronAPI returns the scheduler, or nil when /cron cannot run here.
func cronAPI(rt *Runtime) CronAPI {
	if rt == nil || rt.Cron == nil {
		return nil
	}
	return rt.Cron()
}

// cronMutator returns the scheduler for a change, replying and returning nil
// when the change cannot or may not happen.
func cronMutator(req Request, rt *Runtime) (CronAPI, error) {
	api := cronAPI(rt)
	if api == nil || rt.CanMutate == nil {
		return nil, req.Reply(unavailableMsg)
	}
	if !rt.CanMutate(req.Channel, req.SenderID) {
		return nil, req.Reply(ownerOnlyMsg)
	}
	return api, nil
}

func cronList(_ context.Context, req Request, rt *Runtime) error {
	api := cronAPI(rt)
	if api == nil {
		return req.Reply(unavailableMsg)
	}
	jobs := api.List(req.Channel, req.ChatID)
	if len(jobs) == 0 {
		return req.Reply("No scheduled jobs for this chat. See /cron templates.")
	}
	var b strings.Builder
	b.WriteString("Scheduled jobs:")
	for _, j := range jobs {
		next := "-"
		if !j.NextRun.IsZero() {
			next = j.NextRun.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&b, "\n%s  %s  [%s]  %s  next: %s", shortID(j.ID), j.Name, j.Schedule, j.State, next)
	}
	return req.Reply(b.String())
}

func cronEnable(on bool) Handler {
	return func(_ context.Context, req Request, rt *Runtime) error {
		api, err := cronMutator(req, rt)
		if api == nil {
			return err
		}
		id, ok, err := cronResolveArg(req, api)
		if !ok {
			return err
		}
		if err := api.Enable(id, on); err != nil {
			return req.Reply(fmt.Sprintf("Job %s not changed: %v", shortID(id), err))
		}
		if on {
			return req.Reply(fmt.Sprintf("Job %s resumed.", shortID(id)))
		}
		return req.Reply(fmt.Sprintf("Job %s paused.", shortID(id)))
	}
}

func cronRun(_ context.Context, req Request, rt *Runtime) error {
	api, err := cronMutator(req, rt)
	if api == nil {
		return err
	}
	id, ok, err := cronResolveArg(req, api)
	if !ok {
		return err
	}
	if err := api.RunNow(id); err != nil {
		return req.Reply(fmt.Sprintf("Job %s not queued: %v", shortID(id), err))
	}
	return req.Reply(fmt.Sprintf("Job %s queued.", shortID(id)))
}

func cronHistory(_ context.Context, req Request, rt *Runtime) error {
	api := cronAPI(rt)
	if api == nil {
		return req.Reply(unavailableMsg)
	}
	id, ok, err := cronResolveArg(req, api)
	if !ok {
		return err
	}
	runs := api.History(id, cronHistoryRuns)
	if len(runs) == 0 {
		return req.Reply(fmt.Sprintf("No runs recorded for job %s.", shortID(id)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Last runs of job %s:", shortID(id))
	for _, r := range runs {
		fmt.Fprintf(
			&b,
			"\n%s  %s  %s",
			r.Started.Local().Format("2006-01-02 15:04"),
			r.Duration.Round(time.Millisecond),
			r.Status,
		)
		if r.Error != "" {
			fmt.Fprintf(&b, ": %s", truncateRunes(r.Error, cronHistoryErrMax))
		}
	}
	return req.Reply(b.String())
}

func cronTemplates(_ context.Context, req Request, rt *Runtime) error {
	api := cronAPI(rt)
	if api == nil {
		return req.Reply(unavailableMsg)
	}
	var b strings.Builder
	b.WriteString("Automation templates (create with /cron new <template> key=value ...):")
	for _, t := range api.Templates() {
		fmt.Fprintf(&b, "\n%s  %s", t.ID, t.Title)
		if len(t.Required) > 0 {
			fmt.Fprintf(&b, "  required: %s", strings.Join(t.Required, ", "))
		}
		if len(t.Optional) > 0 {
			fmt.Fprintf(&b, "  optional: %s", strings.Join(t.Optional, ", "))
		}
	}
	return req.Reply(b.String())
}

func cronNew(_ context.Context, req Request, rt *Runtime) error {
	api, err := cronMutator(req, rt)
	if api == nil {
		return err
	}
	args, err := splitArgs(req.Text)
	if err != nil {
		return req.Reply(err.Error())
	}
	// args[0] is the command, args[1] "new".
	if len(args) < 3 {
		return req.Reply(
			`Usage: /cron new <template> key=value ... (quote values with spaces: texto="beber água"). See /cron templates.`,
		)
	}
	tpl := args[2]
	params := map[string]string{}
	for _, kv := range args[3:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return req.Reply(fmt.Sprintf("Bad parameter %q: use key=value", kv))
		}
		params[k] = v
	}
	job, err := api.Create(tpl, params, req.Channel, req.ChatID)
	if err != nil {
		return req.Reply("Job not created: " + err.Error())
	}
	verb := "created"
	if !job.Created {
		verb = "already exists"
	}
	next := "-"
	if !job.NextRun.IsZero() {
		next = job.NextRun.Local().Format("2006-01-02 15:04")
	}
	return req.Reply(
		fmt.Sprintf("Job %s %s: %s [%s], next run: %s", shortID(job.ID), verb, job.Name, job.Schedule, next),
	)
}

// cronResolveArg resolves the <id> argument to a job visible from this chat.
// ok is false when it replied with an error instead.
func cronResolveArg(req Request, api CronAPI) (string, bool, error) {
	arg := nthToken(req.Text, 2)
	if arg == "" {
		return "", false, req.Reply("Give the job id (at least 4 characters; see /cron list).")
	}
	id, err := api.Resolve(arg, req.Channel, req.ChatID)
	if err != nil {
		return "", false, req.Reply(err.Error())
	}
	return id, true, nil
}

func shortID(id string) string {
	if len(id) <= cronShortID {
		return id
	}
	return id[:cronShortID]
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
