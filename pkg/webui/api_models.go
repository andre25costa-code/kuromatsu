package webui

import (
	"net/http"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

// longOpDeadline is the write deadline asked for probe and skill install,
// past the shared server's 30 s write timeout.
const longOpDeadline = 3 * time.Minute

// probeModelList is a seam over setup.ProbeModels for tests.
var probeModelList = setup.ProbeModels

// apiBaseFor returns base, or the provider's default base when base is
// empty and the provider is 9router, whose local address is fixed.
func apiBaseFor(provider, base string) string {
	if base == "" && provider == setup.Kind9Router {
		return providers.DefaultAPIBaseForProtocol(provider)
	}
	return base
}

type modelView struct {
	Alias     string `json:"alias"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	APIBase   string `json:"api_base"`
	HasKey    bool   `json:"has_key"`
	IsDefault bool   `json:"is_default"`
	Enabled   bool   `json:"enabled"`
}

func hasKey(m *config.ModelConfig) bool {
	for _, k := range m.APIKeys {
		if k != nil && k.String() != "" {
			return true
		}
	}
	return false
}

// listModels lists model_list as stored. Keys are never returned, only
// whether an entry has one.
func (s *server) listModels(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadConfigForEdit(s.d.ConfigPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	def := cfg.Agents.Defaults.GetModelName()
	out := make([]modelView, 0, len(cfg.ModelList))
	for _, m := range cfg.ModelList {
		if m == nil {
			continue
		}
		out = append(out, modelView{
			Alias: m.ModelName, Provider: m.Provider, Model: m.Model, APIBase: m.APIBase,
			HasKey: hasKey(m), IsDefault: m.ModelName == def, Enabled: m.Enabled,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out, "default": def})
}

// addModel writes a model entry (setup.ApplyModelChoice) and makes it the
// default. An empty key on an existing entry keeps the stored key: a
// password field cannot show it, so leaving it blank must not erase it.
func (s *server) addModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Alias    string `json:"alias"`
		Provider string `json:"provider"`
		Base     string `json:"base"`
		Key      string `json:"key"`
		Model    string `json:"model"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider := strings.TrimSpace(body.Provider)
	choice := setup.ModelChoice{
		Alias:    strings.TrimSpace(body.Alias),
		Provider: provider,
		Model:    strings.TrimSpace(body.Model),
		APIBase:  apiBaseFor(provider, strings.TrimSpace(body.Base)),
		APIKey:   strings.TrimSpace(body.Key),
	}
	if choice.Alias == "" || choice.Model == "" {
		writeError(w, http.StatusUnprocessableEntity, "alias and model are required")
		return
	}
	s.editConfig(w, func(cfg *config.Config) error {
		var kept config.SecureStrings
		if choice.APIKey == "" {
			for _, m := range cfg.ModelList {
				if m != nil && m.ModelName == choice.Alias {
					kept = m.APIKeys
				}
			}
		}
		setup.ApplyModelChoice(cfg, choice)
		if kept != nil {
			for _, m := range cfg.ModelList {
				if m != nil && m.ModelName == choice.Alias {
					m.APIKeys = kept
				}
			}
		}
		return nil
	})
}

// probeKind maps a model_list provider to the probe's auth style.
func probeKind(provider string) string {
	switch provider {
	case setup.KindOllama, setup.KindAnthropic, setup.KindGemini, setup.KindOpenRouter, setup.Kind9Router:
		return provider
	}
	return setup.KindOpenAI
}

// probeModels asks the provider for its model list. With no key and the
// alias of a stored entry, that entry's key is used server-side.
func (s *server) probeModels(w http.ResponseWriter, r *http.Request) {
	if err := withDeadline(w, longOpDeadline); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot extend the response deadline: "+err.Error())
		return
	}
	var body struct {
		Provider string `json:"provider"`
		Base     string `json:"base"`
		Key      string `json:"key"`
		Alias    string `json:"alias"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider := strings.TrimSpace(body.Provider)
	base := apiBaseFor(provider, strings.TrimSpace(body.Base))
	key := strings.TrimSpace(body.Key)
	if key == "" && body.Alias != "" {
		cfg, err := config.LoadConfigForEdit(s.d.ConfigPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
			return
		}
		for _, m := range cfg.ModelList {
			// The stored key only goes to the stored base: it must not be
			// sent to an address typed in the form.
			if m != nil && m.ModelName == body.Alias &&
				strings.TrimRight(m.APIBase, "/") == strings.TrimRight(base, "/") {
				key = m.APIKey()
			}
		}
	}
	ctx, cancel := opContext(r, time.Minute)
	defer cancel()
	models, err := probeModelList(ctx, probeKind(provider), base, key)
	if err != nil {
		msg := err.Error()
		if key != "" {
			msg = strings.ReplaceAll(msg, key, "***")
		}
		writeError(w, http.StatusBadGateway, msg)
		return
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": ids})
}

func (s *server) setDefaultModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Alias string `json:"alias"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	alias := strings.TrimSpace(body.Alias)
	s.editConfig(w, func(cfg *config.Config) error {
		for _, m := range cfg.ModelList {
			if m != nil && m.ModelName == alias {
				cfg.Agents.Defaults.ModelName = alias
				return nil
			}
		}
		return unprocessable("no model named %q in model_list", alias)
	})
}
