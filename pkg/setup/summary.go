package setup

import (
	"fmt"
	"sort"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// Summary renders the safe overview of cfg shown by the /config chat
// command: model, enabled channels, permission preset, heartbeat, timezone,
// web search, user turn limit and log level, one per line. It never prints
// a secret or an allow_from entry, only how many IDs each channel lists.
// model and provider name the agent's current model; when model is empty the
// configured default is shown.
func Summary(cfg *config.Config, model, provider string) string {
	if cfg == nil {
		return ""
	}
	if model == "" {
		model = cfg.Agents.Defaults.GetModelName()
		if provider == "" {
			provider = cfg.Agents.Defaults.Provider
		}
	}
	if model == "" {
		model = "(none)"
	}
	modelLine := "Model: " + model
	if provider != "" {
		modelLine += " (" + provider + ")"
	}

	heartbeat := "off"
	if cfg.Heartbeat.Enabled {
		heartbeat = fmt.Sprintf("on, every %d min", cfg.Heartbeat.Interval)
	}

	timezone := cfg.Timezone
	if timezone == "" {
		timezone = "system default"
	}

	web := "duckduckgo " + onOff(cfg.Tools.Web.DuckDuckGo.Enabled)
	if !cfg.Tools.Web.Enabled {
		web += " (web tools off)"
	}

	turnLimit := "off"
	if n := cfg.Agents.Defaults.UserTurnMaxMinutes; n > 0 {
		turnLimit = fmt.Sprintf("%d min", n)
	}

	lines := []string{
		modelLine,
		"Channels: " + channelsSummary(cfg.Channels),
		"Permissions: " + string(MatchPermissions(cfg)),
		"Heartbeat: " + heartbeat,
		"Timezone: " + timezone,
		"Web search: " + web,
		"User turn limit: " + turnLimit,
		"Log level: " + config.EffectiveGatewayLogLevel(cfg),
	}
	return strings.Join(lines, "\n")
}

func channelsSummary(channels config.ChannelsConfig) string {
	names := make([]string, 0, len(channels))
	for name, ch := range channels {
		if ch != nil && ch.Enabled {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s (%s)", name, allowFromSummary(channels[name].AllowFrom)))
	}
	return strings.Join(parts, ", ")
}

func allowFromSummary(allowFrom []string) string {
	open := false
	ids := 0
	for _, entry := range allowFrom {
		switch strings.TrimSpace(entry) {
		case "":
		case "*":
			open = true
		default:
			ids++
		}
	}
	plural := func(n int) string {
		if n == 1 {
			return "1 ID"
		}
		return fmt.Sprintf("%d IDs", n)
	}
	switch {
	case open && ids > 0:
		return "open, allow_from: " + plural(ids)
	case open:
		return "open"
	case ids == 0:
		return "closed"
	default:
		return "allow_from: " + plural(ids)
	}
}
