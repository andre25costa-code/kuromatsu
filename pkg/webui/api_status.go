package webui

import (
	"net/http"
	"strconv"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/hwprofile"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

func (s *server) getHeartbeat(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadConfigForEdit(s.d.ConfigPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": cfg.Heartbeat.Enabled, "interval": cfg.Heartbeat.Interval,
	})
}

// setHeartbeat sets heartbeat.enabled and heartbeat.interval (minimum 5)
// with the same checks as /config set.
func (s *server) setHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  *bool `json:"enabled"`
		Interval *int  `json:"interval"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Enabled == nil || body.Interval == nil {
		writeError(w, http.StatusUnprocessableEntity, "enabled and interval are required")
		return
	}
	s.editConfig(w, func(cfg *config.Config) error {
		if _, _, err := setup.SetKey(cfg, "heartbeat.interval", strconv.Itoa(*body.Interval)); err != nil {
			return unprocessable("heartbeat.interval: %v", err)
		}
		if _, _, err := setup.SetKey(cfg, "heartbeat.enabled", strconv.FormatBool(*body.Enabled)); err != nil {
			return unprocessable("heartbeat.enabled: %v", err)
		}
		return nil
	})
}

// status shows version, hardware and profile, the running model and
// channels, and the last 24 h of telemetry as text.
func (s *server) status(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.runtimeConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	out := map[string]any{
		"version":     config.FormatVersion(),
		"model":       cfg.Agents.Defaults.GetModelName(),
		"summary":     setup.Summary(cfg, "", ""),
		"webui_note":  "webui.enabled and webui.allow_remote apply on the next gateway start",
		"stats_error": "",
	}
	var enabled []string
	for _, c := range channelViews(cfg) {
		if c.Enabled {
			enabled = append(enabled, c.Name)
		}
	}
	out["channels"] = enabled
	if s.d.Hardware != nil {
		hw := s.d.Hardware()
		out["hardware"] = map[string]any{
			"total_ram":     hwprofile.FormatGiB(hw.TotalRAM),
			"available_ram": hwprofile.FormatGiB(hw.AvailableRAM),
			"mem_limit":     hwprofile.FormatGiB(hw.MemLimit),
			"cpus":          hw.CPUs,
			"arch":          hw.Arch,
			"os":            hw.OS,
			"native_cpu_ok": hw.NativeCPUOK,
		}
		out["profile"] = map[string]string{
			"detected": string(hwprofile.Select(hw)),
			"config":   string(hwprofile.Match(cfg, hw)),
		}
	}
	if s.d.QueryStats == nil {
		out["stats_error"] = "telemetry unavailable"
	} else if text, qerr := s.d.QueryStats(r.Context(), "", 24); qerr != nil {
		out["stats_error"] = qerr.Error()
	} else {
		out["stats"] = text
	}
	writeJSON(w, http.StatusOK, out)
}
