package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

const ownerOnlyMsg = "Only the owner can change settings here. Add your ID to allow_from or use the web UI."

// configCommand implements /config (a safe summary of the running config)
// and /config set <key> <value> (one of setup.ChatSettableKeys). Changing a
// value needs CanMutate. Reading the summary follows setup.SummaryAllowed:
// on a channel whose allow_from admits everyone it needs CanMutate too, so an
// open bot does not describe its setup to strangers.
func configCommand() Definition {
	return Definition{
		Name:        "config",
		Description: "Show the configuration summary or change a safe setting",
		Usage:       "/config [set <key> <value>]",
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			args, err := splitArgs(req.Text)
			if err != nil {
				return req.Reply(err.Error())
			}
			if len(args) > 1 && strings.EqualFold(args[1], "set") {
				return configSet(req, rt, args[2:])
			}
			if len(args) > 1 {
				return req.Reply("Usage: /config [set <key> <value>]")
			}
			return configSummary(req, rt)
		},
	}
}

func configSummary(req Request, rt *Runtime) error {
	if rt == nil || rt.ConfigSummary == nil {
		return req.Reply(unavailableMsg)
	}
	if !setup.SummaryAllowed(rt.Config, req.Channel, req.SenderID) {
		return req.Reply(ownerOnlyMsg)
	}
	summary, err := rt.ConfigSummary()
	if err != nil {
		return req.Reply("Failed to read configuration: " + err.Error())
	}
	return req.Reply(summary)
}

func configSet(req Request, rt *Runtime, args []string) error {
	if rt == nil || rt.SetConfigKey == nil || rt.CanMutate == nil {
		return req.Reply(unavailableMsg)
	}
	if !rt.CanMutate(req.Channel, req.SenderID) {
		return req.Reply(ownerOnlyMsg)
	}
	if len(args) != 2 {
		return req.Reply(fmt.Sprintf(
			"Usage: /config set <key> <value> (quote values with spaces; \"\" for empty). Keys: %s",
			strings.Join(setup.ChatSettableKeys, ", ")))
	}
	key, value := args[0], args[1]
	if !setup.IsChatSettable(key) {
		return req.Reply(fmt.Sprintf("%s: %s", key, setup.ErrKeyNotSettable.Error()))
	}
	oldValue, newValue, err := rt.SetConfigKey(key, value)
	if err != nil {
		if errors.Is(err, setup.ErrKeyNotSettable) {
			return req.Reply(fmt.Sprintf("%s: %s", key, err.Error()))
		}
		return req.Reply(fmt.Sprintf("%s not changed: %s", key, err.Error()))
	}
	change := fmt.Sprintf("%s: %s → %s", key, shown(oldValue), shown(newValue))
	status := "reloading"
	if rt.ReloadConfig == nil {
		status = "saved; applies on the next start"
	} else if err := rt.ReloadConfig(); errors.Is(err, ErrReloadUnavailable) {
		status = "saved; applies on the next start"
	} else if err != nil {
		status = "saved; reload failed: " + err.Error()
	}
	if key == "timezone" && status == "reloading" {
		// The process clock zone is set once at startup.
		status += "; the clock zone changes after a restart"
	}
	return req.Reply(change + " (" + status + ")")
}

func shown(v string) string {
	if v == "" {
		return `""`
	}
	return v
}

// splitArgs splits a command line on whitespace, keeping double-quoted text
// together: `k="a b"` is one argument `k=a b`, and `""` is an empty
// argument. Typographic quotes (“ ”), which phone keyboards insert, count
// as double quotes.
func splitArgs(text string) ([]string, error) {
	var (
		args     []string
		cur      strings.Builder
		inQuote  bool
		hasToken bool
	)
	for _, r := range text {
		switch {
		case r == '"' || r == '“' || r == '”':
			inQuote = !inQuote
			hasToken = true
		case !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			if hasToken {
				args = append(args, cur.String())
				cur.Reset()
				hasToken = false
			}
		default:
			cur.WriteRune(r)
			hasToken = true
		}
	}
	if inQuote {
		return nil, errors.New("unterminated quote")
	}
	if hasToken {
		args = append(args, cur.String())
	}
	return args, nil
}
