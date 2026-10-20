// Package automation holds the built-in catalog of beginner automation
// templates: ready-made scheduled jobs (a reminder, a daily summary, a page
// monitor, a daily news search) that a user creates by filling in a few
// parameters, from the /cron chat command or the web UI, without editing
// files. Each template renders to a cron job (name, schedule, payload) for
// cron.CronService.AddJobWithPayload.
package automation

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

// Param kinds.
const (
	KindText     = "text"     // free text, not empty
	KindTime     = "time"     // HH:MM, 24-hour clock
	KindDatetime = "datetime" // RFC3339, or HH:MM meaning its next occurrence (today or tomorrow)
	KindURL      = "url"      // absolute http or https URL
	KindInt      = "int"      // decimal integer; the template sets the range
)

// Param is one input of a template.
type Param struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Required bool   `json:"required,omitempty"`
	// Default is used when the parameter is missing or empty.
	Default string `json:"default,omitempty"`
}

// Template describes one catalog entry.
type Template struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Params      []Param `json:"params"`
	// Requires lists the config keys (boolean switches such as
	// "tools.web.enabled") that must be on for jobs from this template to
	// run. Check them with Unmet before creating a job.
	Requires []string `json:"requires"`
}

// entry is a catalog item as embedded: the public Template plus the payload
// message (with {param} placeholders) and focus window.
type entry struct {
	Template
	Message string `json:"message"`
	Window  string `json:"window,omitempty"`
}

//go:embed templates.json
var catalogJSON []byte

var catalog = mustLoadCatalog(catalogJSON)

func mustLoadCatalog(data []byte) []entry {
	entries, err := loadCatalog(data)
	if err != nil {
		panic("automation: invalid embedded templates.json: " + err.Error())
	}
	return entries
}

func loadCatalog(data []byte) ([]entry, error) {
	var file struct {
		Templates []entry `json:"templates"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range file.Templates {
		if e.ID == "" || seen[e.ID] {
			return nil, fmt.Errorf("missing or duplicate template id %q", e.ID)
		}
		seen[e.ID] = true
		if e.Message == "" {
			return nil, fmt.Errorf("template %q has no message", e.ID)
		}
		for _, p := range e.Params {
			switch p.Kind {
			case KindText, KindTime, KindDatetime, KindURL, KindInt:
			default:
				return nil, fmt.Errorf("template %q param %q has unknown kind %q", e.ID, p.Name, p.Kind)
			}
		}
		for _, key := range e.Requires {
			if _, ok := requirementChecks[key]; !ok {
				return nil, fmt.Errorf("template %q requires unknown key %q", e.ID, key)
			}
		}
	}
	return file.Templates, nil
}

// List returns the catalog in its fixed order.
func List() []Template {
	out := make([]Template, len(catalog))
	for i, e := range catalog {
		out[i] = e.Template
		out[i].Params = append([]Param(nil), e.Params...)
		out[i].Requires = append([]string(nil), e.Requires...)
	}
	return out
}

// Get returns the template with the given id.
func Get(id string) (Template, bool) {
	for _, t := range List() {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

var requirementChecks = map[string]func(*config.Config) bool{
	"tools.cron.enabled":      func(c *config.Config) bool { return c.Tools.Cron.Enabled },
	"tools.web.enabled":       func(c *config.Config) bool { return c.Tools.Web.Enabled },
	"tools.web_fetch.enabled": func(c *config.Config) bool { return c.Tools.WebFetch.Enabled },
}

// Unmet returns the keys of t.Requires that are off in cfg, in order.
func Unmet(t Template, cfg *config.Config) []string {
	var missing []string
	for _, key := range t.Requires {
		check := requirementChecks[key]
		if cfg == nil || check == nil || !check(cfg) {
			missing = append(missing, key)
		}
	}
	return missing
}

// Render validates params against template id and builds the job: its name,
// schedule, payload (delivered to channel/to) and whether it is deleted after
// its single run. loc interprets HH:MM and RFC3339 inputs and is the zone of
// daily schedules: pass time.LoadLocation(cfg.Timezone), or time.Local when
// that is empty. A daily schedule records loc's name as its TZ, except that
// time.Local yields an empty TZ (the process timezone), never "Local".
func Render(
	id string,
	params map[string]string,
	loc *time.Location,
	now time.Time,
	channel, to string,
) (name string, sched cron.CronSchedule, payload cron.CronPayload, deleteAfterRun bool, err error) {
	var e *entry
	for i := range catalog {
		if catalog[i].ID == id {
			e = &catalog[i]
			break
		}
	}
	if e == nil {
		return "", sched, payload, false, fmt.Errorf("unknown template %q", id)
	}
	if loc == nil {
		loc = time.Local
	}
	values, err := e.values(params)
	if err != nil {
		return "", sched, payload, false, err
	}

	switch e.ID {
	case "lembrete":
		quando, todoDia := values["quando"], values["todo_dia"]
		switch {
		case quando != "" && todoDia != "":
			return "", sched, payload, false, errors.New("give either quando or todo_dia, not both")
		case quando != "":
			at, perr := parseDatetime(quando, loc, now)
			if perr != nil {
				return "", sched, payload, false, perr
			}
			if !at.After(now) {
				return "", sched, payload, false, fmt.Errorf("quando %q is in the past", quando)
			}
			ms := at.UnixMilli()
			sched = cron.CronSchedule{Kind: "at", AtMS: &ms}
			deleteAfterRun = true
		case todoDia != "":
			sched = dailySchedule(todoDia, loc)
		default:
			return "", sched, payload, false, errors.New("quando or todo_dia is required")
		}
		name = "Lembrete: " + truncate(values["texto"], 40)
	case "resumo-diario":
		sched = dailySchedule(values["hora"], loc)
		name = "Resumo diário"
	case "monitorar-pagina":
		hours, _ := strconv.Atoi(values["a_cada_horas"])
		if hours < 1 || hours > 24 {
			return "", sched, payload, false, fmt.Errorf("a_cada_horas must be between 1 and 24, got %d", hours)
		}
		every := int64(hours) * 3600000
		sched = cron.CronSchedule{Kind: "every", EveryMS: &every}
		u, _ := url.Parse(values["url"])
		name = "Monitorar " + u.Host
	case "pesquisa-diaria":
		sched = dailySchedule(values["hora"], loc)
		name = "Notícias: " + truncate(values["tema"], 40)
	default:
		return "", sched, payload, false, fmt.Errorf("template %q has no renderer", e.ID)
	}

	payload = cron.CronPayload{
		Message: e.render(values),
		Channel: channel,
		To:      to,
		Window:  e.Window,
	}
	return name, sched, payload, deleteAfterRun, nil
}

// values checks params against the template's declared params and returns
// them with defaults applied.
func (e *entry) values(params map[string]string) (map[string]string, error) {
	known := map[string]bool{}
	for _, p := range e.Params {
		known[p.Name] = true
	}
	var unknown []string
	for k := range params {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown parameter(s) for %s: %s", e.ID, strings.Join(unknown, ", "))
	}

	out := map[string]string{}
	for _, p := range e.Params {
		v := strings.TrimSpace(params[p.Name])
		if v == "" {
			v = p.Default
		}
		if v == "" {
			if p.Required {
				return nil, fmt.Errorf("parameter %s is required", p.Name)
			}
			continue
		}
		if err := checkKind(p, v); err != nil {
			return nil, err
		}
		out[p.Name] = v
	}
	return out, nil
}

func (e *entry) render(values map[string]string) string {
	pairs := make([]string, 0, 2*len(e.Params))
	for _, p := range e.Params {
		pairs = append(pairs, "{"+p.Name+"}", values[p.Name])
	}
	return strings.NewReplacer(pairs...).Replace(e.Message)
}

var hhmmRe = regexp.MustCompile(`^([01]?[0-9]|2[0-3]):([0-5][0-9])$`)

func checkKind(p Param, v string) error {
	switch p.Kind {
	case KindTime:
		if !hhmmRe.MatchString(v) {
			return fmt.Errorf("%s: %q is not a time in HH:MM", p.Name, v)
		}
	case KindDatetime:
		if !hhmmRe.MatchString(v) {
			if _, err := time.Parse(time.RFC3339, v); err != nil {
				return fmt.Errorf("%s: %q is neither HH:MM nor an RFC3339 date-time", p.Name, v)
			}
		}
	case KindURL:
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s: %q is not an http or https URL", p.Name, v)
		}
	case KindInt:
		if _, err := strconv.Atoi(v); err != nil {
			return fmt.Errorf("%s: %q is not a whole number", p.Name, v)
		}
	}
	return nil
}

// parseDatetime reads an RFC3339 date-time, or HH:MM as its next occurrence
// in loc after now (today, or tomorrow when that time has passed).
func parseDatetime(v string, loc *time.Location, now time.Time) (time.Time, error) {
	if m := hhmmRe.FindStringSubmatch(v); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		local := now.In(loc)
		at := time.Date(local.Year(), local.Month(), local.Day(), h, mi, 0, 0, loc)
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at, nil
	}
	return time.Parse(time.RFC3339, v)
}

// dailySchedule is a cron schedule for every day at hhmm (already checked).
func dailySchedule(hhmm string, loc *time.Location) cron.CronSchedule {
	m := hhmmRe.FindStringSubmatch(hhmm)
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	return cron.CronSchedule{Kind: "cron", Expr: fmt.Sprintf("%d %d * * *", mi, h), TZ: tzName(loc)}
}

// tzName is the TZ stored for loc: empty for the process timezone, so the
// job never records the invalid name "Local".
func tzName(loc *time.Location) string {
	if loc == nil || loc == time.Local {
		return ""
	}
	if name := loc.String(); name != "Local" {
		return name
	}
	return ""
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
