package webui

import (
	"net/http"
	"sort"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

type channelView struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Enabled   bool     `json:"enabled"`
	AllowFrom []string `json:"allow_from"`
}

// channelViews lists the channels (name order) with no setting other than
// enabled, type and allow_from, so no secret can leak.
func channelViews(cfg *config.Config) []channelView {
	out := make([]channelView, 0, len(cfg.Channels))
	for name, ch := range cfg.Channels {
		if ch == nil {
			continue
		}
		typ := ch.Type
		if typ == "" {
			typ = name
		}
		allow := append([]string{}, ch.AllowFrom...)
		out = append(out, channelView{Name: name, Type: typ, Enabled: ch.Enabled, AllowFrom: allow})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *server) listChannels(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadConfigForEdit(s.d.ConfigPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": channelViews(cfg)})
}

// setAllowFrom replaces a channel's allow_from. An empty list on an enabled
// channel is refused: it would reject every sender.
func (s *server) setAllowFrom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AllowFrom []string `json:"allow_from"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := r.PathValue("name")
	list := make([]string, 0, len(body.AllowFrom))
	for _, e := range body.AllowFrom {
		if e = strings.TrimSpace(e); e != "" {
			list = append(list, e)
		}
	}
	s.editConfig(w, func(cfg *config.Config) error {
		ch, ok := cfg.Channels[name]
		if !ok || ch == nil {
			return notFound("no channel %q", name)
		}
		if ch.Enabled && len(list) == 0 {
			return unprocessable("allow_from cannot be empty on an enabled channel: it would reject every sender")
		}
		ch.AllowFrom = list
		return nil
	})
}

// permissionKeys are the six keys a preset sets, in the UI's order.
type permissionKeys struct {
	RestrictToWorkspace bool `json:"restrict_to_workspace"`
	ExecEnabled         bool `json:"exec_enabled"`
	ExecAllowRemote     bool `json:"exec_allow_remote"`
	WebEnabled          bool `json:"web_enabled"`
	CronEnabled         bool `json:"cron_enabled"`
	CronAllowCommand    bool `json:"cron_allow_command"`
}

func readPermissions(cfg *config.Config) permissionKeys {
	return permissionKeys{
		RestrictToWorkspace: cfg.Agents.Defaults.RestrictToWorkspace,
		ExecEnabled:         cfg.Tools.Exec.Enabled,
		ExecAllowRemote:     cfg.Tools.Exec.AllowRemote,
		WebEnabled:          cfg.Tools.Web.Enabled,
		CronEnabled:         cfg.Tools.Cron.Enabled,
		CronAllowCommand:    cfg.Tools.Cron.AllowCommand,
	}
}

func (s *server) getPermissions(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadConfigForEdit(s.d.ConfigPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"preset":  setup.MatchPermissions(cfg),
		"values":  readPermissions(cfg),
		"presets": setup.Presets,
	})
}

// setPermissions applies a preset ({preset}) or the six keys ({custom}).
func (s *server) setPermissions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Preset string          `json:"preset"`
		Custom *permissionKeys `json:"custom"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if (body.Preset == "") == (body.Custom == nil) {
		writeError(w, http.StatusUnprocessableEntity, "give either preset or custom")
		return
	}
	s.editConfig(w, func(cfg *config.Config) error {
		if body.Custom == nil {
			if err := setup.ApplyPermissions(cfg, setup.Preset(body.Preset)); err != nil {
				return unprocessable("%v", err)
			}
			return nil
		}
		c := body.Custom
		cfg.Agents.Defaults.RestrictToWorkspace = c.RestrictToWorkspace
		cfg.Tools.Exec.Enabled = c.ExecEnabled
		cfg.Tools.Exec.AllowRemote = c.ExecAllowRemote
		cfg.Tools.Web.Enabled = c.WebEnabled
		cfg.Tools.Cron.Enabled = c.CronEnabled
		cfg.Tools.Cron.AllowCommand = c.CronAllowCommand
		return nil
	})
}
