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
//   - "123456": the ID itself. A bare entry counts when it is numeric (an
//     optional leading "-" and digits) or an address-style ID with digits and
//     an "@" after its first character, such as a WhatsApp JID
//     ("5511999999999@s.whatsapp.net"). Any other bare entry, such as
//     "alice", is a username and yields nothing.
//   - "telegram:123456": the part after the colon, only when the platform
//     equals channelType (case-insensitive) and that part has the shape of a
//     bare ID above, so "telegram:alice" yields nothing. A prefix that is not
//     a channel type name (it has digits) is not a platform:
//     "5511:12@s.whatsapp.net" is a bare ID.
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
	if platform, id, ok := splitPlatform(entry); ok {
		if channelType == "" || !strings.EqualFold(platform, channelType) {
			return "", false
		}
		if !isNumericID(id) && !isAddressID(id) {
			return "", false
		}
		return id, true
	}
	if !isNumericID(entry) && !isAddressID(entry) {
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

// SummaryAllowed is the rule for reading the /config summary from a chat:
// internal channels always may. Any other channel is looked up by its name
// in cfg.Channels, the key the channel manager registers it under; a channel
// that cannot be found there may not (fail closed). A channel whose
// allow_from admits everyone ("*") shows it only to senders CanMutate
// accepts; any other channel to every sender it lets in.
func SummaryAllowed(cfg *config.Config, channel, senderID string) bool {
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
	for _, entry := range ch.AllowFrom {
		if strings.TrimSpace(entry) == "*" {
			return CanMutate(cfg, channel, senderID)
		}
	}
	return true
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
	if platform, id, ok := splitPlatform(senderID); ok {
		if !strings.EqualFold(platform, channelType) {
			return "", false
		}
		senderID = id
	}
	if senderID == "" {
		return "", false
	}
	return senderID, true
}

// splitPlatform splits "<platform>:<id>" only when the prefix looks like a
// channel type name (letters, "_" or "-"), so an ID that itself contains a
// colon, such as a WhatsApp JID with a device part, is kept whole.
func splitPlatform(s string) (platform, id string, ok bool) {
	platform, id, ok = identity.ParseCanonicalID(s)
	if !ok {
		return "", "", false
	}
	for _, r := range platform {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' && r != '-' {
			return "", "", false
		}
	}
	return platform, strings.TrimSpace(id), true
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

// isAddressID reports whether s looks like an address-style platform ID: an
// "@" after the first character and at least one digit.
func isAddressID(s string) bool {
	return strings.Index(s, "@") > 0 && strings.ContainsAny(s, "0123456789")
}
