package setup

import (
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/constants"
	"github.com/andre25costa-code/kuromatsu/pkg/identity"
)

// OwnerID extracts the platform ID an allow_from entry names, for deciding
// who may change settings from a chat. Only an ID counts, never a username,
// because a username can change hands:
//
//   - "123456": the ID itself. A bare entry counts only when it is numeric
//     (an optional leading "-" and digits); any other bare entry, such as
//     "alice", is a username and yields nothing. An ID that is not numeric
//     (a WhatsApp JID, for example) is written as "<platform>:<id>".
//   - "telegram:123456": the part after the colon, only when the platform
//     equals channelType (case-insensitive).
//   - "123456|alice": the part before the "|", under the same rules.
//   - "@alice", "*" and empty entries yield nothing.
func OwnerID(entry, channelType string) (string, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" || entry == "*" || strings.HasPrefix(entry, "@") {
		return "", false
	}
	if idx := strings.Index(entry, "|"); idx >= 0 {
		entry = strings.TrimSpace(entry[:idx])
	}
	if platform, id, ok := identity.ParseCanonicalID(entry); ok {
		if channelType == "" || !strings.EqualFold(platform, channelType) {
			return "", false
		}
		id = strings.TrimSpace(id)
		if id == "" || strings.HasPrefix(id, "@") {
			return "", false
		}
		return id, true
	}
	if !isNumericID(entry) {
		return "", false
	}
	return entry, true
}

// IsOwner reports whether senderID matches the ID of an allow_from entry
// (see OwnerID). A sender given as "<platform>:<id>" or "<id>|<username>" is
// reduced to its ID first; a "<platform>:<id>" sender of another platform
// never matches.
func IsOwner(allowFrom []string, channelType, senderID string) bool {
	sender, ok := senderOwnerID(senderID, channelType)
	if !ok {
		return false
	}
	for _, entry := range allowFrom {
		if id, ok := OwnerID(entry, channelType); ok && id == sender {
			return true
		}
	}
	return false
}

// CanMutate is the rule for changing settings or scheduled jobs from a chat:
// internal channels (cli, system, subagent) always may; any other channel
// only when the sender's ID is listed in that channel's allow_from.
func CanMutate(cfg *config.Config, channel, senderID string) bool {
	if constants.IsInternalChannel(channel) {
		return true
	}
	if cfg == nil {
		return false
	}
	ch := cfg.Channels[channel]
	if ch == nil {
		return false
	}
	return IsOwner(ch.AllowFrom, channelType(channel, ch), senderID)
}

// OpenChannel reports whether channel's allow_from admits everyone ("*").
func OpenChannel(cfg *config.Config, channel string) bool {
	if cfg == nil {
		return false
	}
	ch := cfg.Channels[channel]
	if ch == nil {
		return false
	}
	for _, entry := range ch.AllowFrom {
		if strings.TrimSpace(entry) == "*" {
			return true
		}
	}
	return false
}

func channelType(name string, ch *config.Channel) string {
	if ch != nil && ch.Type != "" {
		return ch.Type
	}
	return name
}

func senderOwnerID(senderID, channelType string) (string, bool) {
	senderID = strings.TrimSpace(senderID)
	if idx := strings.Index(senderID, "|"); idx >= 0 {
		senderID = strings.TrimSpace(senderID[:idx])
	}
	if platform, id, ok := identity.ParseCanonicalID(senderID); ok {
		if !strings.EqualFold(platform, channelType) {
			return "", false
		}
		senderID = strings.TrimSpace(id)
	}
	if senderID == "" {
		return "", false
	}
	return senderID, true
}

func isNumericID(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
