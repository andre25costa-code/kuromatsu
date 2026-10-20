package automation

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // zone names in tests resolve on any OS

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
)

var testNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func TestRender_Validation(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		params  map[string]string
		wantErr string
	}{
		{"unknown template", "email", nil, "unknown template"},
		{"missing texto", "lembrete", map[string]string{"quando": "13:00"}, "texto is required"},
		{"missing when", "lembrete", map[string]string{"texto": "x"}, "quando or todo_dia"},
		{"both when", "lembrete", map[string]string{"texto": "x", "quando": "13:00", "todo_dia": "08:00"}, "not both"},
		{"bad HH:MM", "lembrete", map[string]string{"texto": "x", "todo_dia": "25:00"}, "HH:MM"},
		{"bad HH:MM minutes", "resumo-diario", map[string]string{"hora": "8:7"}, "HH:MM"},
		{"bad datetime", "lembrete", map[string]string{"texto": "x", "quando": "amanhã"}, "RFC3339"},
		{"past datetime", "lembrete", map[string]string{"texto": "x", "quando": "2020-01-01T00:00:00Z"}, "in the past"},
		{"missing url", "monitorar-pagina", map[string]string{"condicao": "c"}, "url is required"},
		{"ftp url", "monitorar-pagina", map[string]string{"url": "ftp://x.org", "condicao": "c"}, "http or https"},
		{"relative url", "monitorar-pagina", map[string]string{"url": "x.org/a", "condicao": "c"}, "http or https"},
		{"missing condicao", "monitorar-pagina", map[string]string{"url": "https://x.org"}, "condicao is required"},
		{
			"hours 0",
			"monitorar-pagina",
			map[string]string{"url": "https://x.org", "condicao": "c", "a_cada_horas": "0"},
			"between 1 and 24",
		},
		{
			"hours 25",
			"monitorar-pagina",
			map[string]string{"url": "https://x.org", "condicao": "c", "a_cada_horas": "25"},
			"between 1 and 24",
		},
		{
			"hours text",
			"monitorar-pagina",
			map[string]string{"url": "https://x.org", "condicao": "c", "a_cada_horas": "seis"},
			"whole number",
		},
		{"missing tema", "pesquisa-diaria", nil, "tema is required"},
		{"unknown param", "resumo-diario", map[string]string{"horario": "08:00"}, "unknown parameter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := Render(tc.id, tc.params, time.UTC, testNow, "telegram", "42")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Render error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestRender_Defaults(t *testing.T) {
	name, sched, payload, del, err := Render("resumo-diario", nil, time.UTC, testNow, "telegram", "42")
	if err != nil {
		t.Fatal(err)
	}
	if sched.Kind != "cron" || sched.Expr != "0 8 * * *" {
		t.Fatalf("schedule = %+v, want cron 0 8 * * *", sched)
	}
	if del || name == "" || payload.Channel != "telegram" || payload.To != "42" {
		t.Fatalf("job = %q del=%v payload=%+v", name, del, payload)
	}
	if !strings.Contains(payload.Message, "Sem pendências") {
		t.Fatalf("message = %q", payload.Message)
	}

	sched, err = renderSchedule("pesquisa-diaria", map[string]string{"tema": "Go"}, time.UTC, testNow)
	if err != nil || sched.Expr != "30 7 * * *" {
		t.Fatalf("pesquisa-diaria schedule = %+v, %v; want 30 7 * * *", sched, err)
	}

	_, sched, payload, _, err = Render(
		"monitorar-pagina",
		map[string]string{
			"url":      "https://example.org/p",
			"condicao": "o preço cair",
		},
		time.UTC,
		testNow,
		"cli",
		"direct",
	)
	if err != nil || sched.Kind != "every" || sched.EveryMS == nil || *sched.EveryMS != 6*3600000 {
		t.Fatalf("monitorar-pagina schedule = %+v, %v; want every 6h", sched, err)
	}
	msg := payload.Message
	if !strings.Contains(msg, "https://example.org/p") || !strings.Contains(msg, "o preço cair") ||
		!strings.Contains(payload.Message, `"NADA"`) ||
		strings.Contains(payload.Message, "{") {
		t.Fatalf("message = %q", payload.Message)
	}

	// lembrete once: HH:MM already past today means tomorrow; deleted after run.
	name, sched, payload, del, err = Render("lembrete",
		map[string]string{"texto": "beber água", "quando": "09:30"}, time.UTC, testNow, "telegram", "42")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC).UnixMilli()
	if sched.Kind != "at" || sched.AtMS == nil || *sched.AtMS != want || !del {
		t.Fatalf("lembrete schedule = %+v del=%v, want at %d and delete after run", sched, del, want)
	}
	if payload.Message != "Envie ao usuário exatamente este lembrete, sem comentários: beber água" {
		t.Fatalf("message = %q", payload.Message)
	}
	if !strings.Contains(name, "beber água") {
		t.Fatalf("name = %q", name)
	}

	_, sched, _, del, err = Render("lembrete",
		map[string]string{"texto": "x", "todo_dia": "21:05"}, time.UTC, testNow, "telegram", "42")
	if err != nil || sched.Kind != "cron" || sched.Expr != "5 21 * * *" || del {
		t.Fatalf("lembrete daily = %+v del=%v err=%v", sched, del, err)
	}
}

func TestRender_WebWindow(t *testing.T) {
	for _, tc := range []struct {
		id     string
		params map[string]string
		window string
	}{
		{"monitorar-pagina", map[string]string{"url": "https://example.org", "condicao": "c"}, "web"},
		{"pesquisa-diaria", map[string]string{"tema": "Go"}, "web"},
		{"resumo-diario", nil, ""},
		{"lembrete", map[string]string{"texto": "x", "todo_dia": "08:00"}, ""},
	} {
		_, _, payload, _, err := Render(tc.id, tc.params, time.UTC, testNow, "cli", "direct")
		if err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		if payload.Window != tc.window {
			t.Fatalf("%s window = %q, want %q", tc.id, payload.Window, tc.window)
		}
	}

	monitor, _ := Get("monitorar-pagina")
	cfg := config.DefaultConfig()
	cfg.Tools.Cron.Enabled = true
	cfg.Tools.WebFetch.Enabled = true
	cfg.Tools.Web.Enabled = false
	if got := Unmet(monitor, cfg); len(got) != 1 || got[0] != "tools.web.enabled" {
		t.Fatalf("Unmet = %v, want [tools.web.enabled]", got)
	}
	cfg.Tools.Web.Enabled = true
	if got := Unmet(monitor, cfg); len(got) != 0 {
		t.Fatalf("Unmet = %v, want none", got)
	}
}

func TestList_EmbeddedCatalogValid(t *testing.T) {
	entries, err := loadCatalog(catalogJSON)
	if err != nil {
		t.Fatalf("embedded catalog: %v", err)
	}
	list := List()
	want := []string{"lembrete", "resumo-diario", "monitorar-pagina", "pesquisa-diaria"}
	if len(list) != len(want) || len(entries) != len(want) {
		t.Fatalf("List() has %d templates, want %d", len(list), len(want))
	}
	for i, id := range want {
		if list[i].ID != id {
			t.Fatalf("List()[%d] = %q, want %q", i, list[i].ID, id)
		}
		if list[i].Title == "" || list[i].Description == "" || len(list[i].Requires) == 0 {
			t.Fatalf("template %q lacks title, description or requires", id)
		}
		for _, p := range list[i].Params {
			switch p.Kind {
			case KindText, KindTime, KindDatetime, KindURL, KindInt:
			default:
				t.Fatalf("template %q param %q kind %q", id, p.Name, p.Kind)
			}
			if p.Default != "" {
				if err := checkKind(p, p.Default); err != nil {
					t.Fatalf("template %q param %q default: %v", id, p.Name, err)
				}
			}
		}
	}
	// List hands out copies: changing one does not change the catalog.
	list[0].Params[0].Name = "changed"
	if List()[0].Params[0].Name == "changed" {
		t.Fatal("List() exposes the catalog's own slices")
	}
	if _, err := loadCatalog(
		[]byte(`{"templates":[{"id":"a","message":"m","params":[{"name":"p","kind":"email"}]}]}`),
	); err == nil {
		t.Fatal("loadCatalog accepted an unknown param kind")
	}
}

func TestRender_EmptyTimezoneIsNotLocal(t *testing.T) {
	sched, err := renderSchedule("resumo-diario", nil, time.Local, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if sched.TZ != "" {
		t.Fatalf("TZ = %q with the process timezone, want empty", sched.TZ)
	}
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	sched, err = renderSchedule("resumo-diario", nil, sp, testNow)
	if err != nil || sched.TZ != "America/Sao_Paulo" {
		t.Fatalf("TZ = %q, %v; want America/Sao_Paulo", sched.TZ, err)
	}
	// HH:MM is read in the given zone.
	sched, err = renderSchedule(
		"lembrete",
		map[string]string{"texto": "x", "quando": "10:00"},
		sp,
		testNow,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 15, 10, 0, 0, 0, sp) // 13:00 UTC, after testNow
	if *sched.AtMS != want.UnixMilli() {
		t.Fatalf("at = %v, want %v", time.UnixMilli(*sched.AtMS).UTC(), want.UTC())
	}
}

// renderSchedule renders a template for a cli chat and returns its schedule.
func renderSchedule(id string, params map[string]string, loc *time.Location, now time.Time) (cron.CronSchedule, error) {
	_, sched, _, _, err := Render( //nolint:dogsled // only the schedule matters here
		id,
		params,
		loc,
		now,
		"cli",
		"direct",
	)
	return sched, err
}
