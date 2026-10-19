package setup

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// ChatSettableKeys are the only config keys SetKey changes: plain values with
// no security effect. Security keys (allow_from, exec, workspace restriction,
// API keys) are never settable this way.
var ChatSettableKeys = []string{
	"heartbeat.enabled",
	"heartbeat.interval",
	"agents.defaults.user_turn_max_minutes",
	"tools.web.duckduckgo.enabled",
	"gateway.log_level",
	"timezone",
}

// ErrKeyNotSettable is returned by SetKey for a key outside ChatSettableKeys.
var ErrKeyNotSettable = errors.New("not settable from chat; use the web UI or `kuromatsu` CLI")

// IsChatSettable reports whether key is in ChatSettableKeys.
func IsChatSettable(key string) bool {
	return slices.Contains(ChatSettableKeys, key)
}

// SetKey validates value for key and writes it to cfg, returning the old and
// new values as text. cfg is not changed when an error is returned.
//
//   - heartbeat.enabled, tools.web.duckduckgo.enabled: on/off/true/false
//   - heartbeat.interval: integer minutes, minimum 5
//   - agents.defaults.user_turn_max_minutes: integer, minimum 0 (0 = off)
//   - gateway.log_level: debug, info, warn or error
//   - timezone: a zone name time.LoadLocation resolves, or "" for the
//     process timezone. "Local" is refused: it is not a zone name and would
//     mean different zones on different machines.
func SetKey(cfg *config.Config, key, value string) (oldValue, newValue string, err error) {
	if cfg == nil {
		return "", "", errors.New("config is nil")
	}
	value = strings.TrimSpace(value)
	switch key {
	case "heartbeat.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", "", err
		}
		oldValue = onOff(cfg.Heartbeat.Enabled)
		cfg.Heartbeat.Enabled = b
		return oldValue, onOff(b), nil
	case "heartbeat.interval":
		n, err := parseIntMin(value, 5)
		if err != nil {
			return "", "", err
		}
		oldValue = strconv.Itoa(cfg.Heartbeat.Interval)
		cfg.Heartbeat.Interval = n
		return oldValue, strconv.Itoa(n), nil
	case "agents.defaults.user_turn_max_minutes":
		n, err := parseIntMin(value, 0)
		if err != nil {
			return "", "", err
		}
		oldValue = strconv.Itoa(cfg.Agents.Defaults.UserTurnMaxMinutes)
		cfg.Agents.Defaults.UserTurnMaxMinutes = n
		return oldValue, strconv.Itoa(n), nil
	case "tools.web.duckduckgo.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", "", err
		}
		oldValue = onOff(cfg.Tools.Web.DuckDuckGo.Enabled)
		cfg.Tools.Web.DuckDuckGo.Enabled = b
		return oldValue, onOff(b), nil
	case "gateway.log_level":
		level := strings.ToLower(value)
		switch level {
		case "debug", "info", "warn", "error":
		default:
			return "", "", fmt.Errorf("invalid log level %q: use debug, info, warn or error", value)
		}
		oldValue = cfg.Gateway.LogLevel
		cfg.Gateway.LogLevel = level
		return oldValue, level, nil
	case "timezone":
		if value != "" {
			if value == "Local" {
				return "", "", errors.New(
					`"Local" is not a timezone name; use an IANA name or "" for the system timezone`,
				)
			}
			if _, err := time.LoadLocation(value); err != nil {
				return "", "", fmt.Errorf("unknown timezone %q: use an IANA name such as America/Sao_Paulo", value)
			}
		}
		oldValue = cfg.Timezone
		cfg.Timezone = value
		return oldValue, value, nil
	default:
		return "", "", ErrKeyNotSettable
	}
}

// ConfigMu serializes every load-change-save of the config file in this
// process (SaveKey from chat, the web UI), so two edits never interleave
// their load and save. Hold it from the load to the save.
var ConfigMu sync.Mutex

// SaveKey changes one ChatSettableKeys key in the config file at path: it
// loads the file as stored (LoadConfigForEdit, so environment values never
// reach the disk), applies SetKey, checks the result with
// config.ValidateForSave and saves it. Nothing is written when any step
// fails. The caller applies the change (for example with a gateway reload).
func SaveKey(path, key, value string) (oldValue, newValue string, err error) {
	if path == "" {
		return "", "", errors.New("config file path is unknown")
	}
	if !IsChatSettable(key) {
		return "", "", ErrKeyNotSettable
	}
	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	cfg, err := config.LoadConfigForEdit(path)
	if err != nil {
		return "", "", fmt.Errorf("load config: %w", err)
	}
	if oldValue, newValue, err = SetKey(cfg, key, value); err != nil {
		return "", "", err
	}
	if err = config.ValidateForSave(cfg); err != nil {
		return "", "", fmt.Errorf("the change would make the config invalid: %w", err)
	}
	if err = config.SaveConfig(path, cfg); err != nil {
		return "", "", fmt.Errorf("save config: %w", err)
	}
	return oldValue, newValue, nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "on", "true":
		return true, nil
	case "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("invalid value %q: use on, off, true or false", v)
}

func parseIntMin(v string, minimum int) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", v)
	}
	if n < minimum {
		return 0, fmt.Errorf("invalid value %d: minimum %d", n, minimum)
	}
	return n, nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
