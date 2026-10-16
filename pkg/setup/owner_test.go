package setup

import (
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestIsOwner_Table(t *testing.T) {
	cases := []struct {
		name      string
		allowFrom []string
		chType    string
		sender    string
		want      bool
	}{
		{"bare numeric ID", []string{"123"}, "telegram", "123", true},
		{"canonical ID of the same platform", []string{"telegram:123"}, "telegram", "123", true},
		{"canonical ID, platform case differs", []string{"Telegram:123"}, "telegram", "123", true},
		{"ID with username", []string{"123|alice"}, "telegram", "123", true},
		{"canonical ID with username", []string{"telegram:123|alice"}, "telegram", "123", true},
		{"canonical ID of another platform", []string{"whatsapp:123"}, "telegram", "123", false},
		{"username with @", []string{"@alice"}, "telegram", "123", false},
		{"bare username", []string{"alice"}, "telegram", "123", false},
		{"bare username equal to sender", []string{"alice"}, "irc", "alice", false},
		{"username with @ and compound sender", []string{"@alice"}, "telegram", "123|alice", false},
		{"wildcard", []string{"*"}, "telegram", "123", false},
		{"other ID", []string{"124"}, "telegram", "123", false},
		{"wildcard plus owner", []string{"*", "123"}, "telegram", "123", true},
		{"canonical sender", []string{"123"}, "telegram", "telegram:123", true},
		{"canonical sender of another platform", []string{"123"}, "telegram", "discord:123", false},
		{"compound sender", []string{"123"}, "telegram", "123|alice", true},
		{"negative numeric ID", []string{"-100123"}, "telegram", "-100123", true},
		{
			"JID through canonical form",
			[]string{"whatsapp:5511999@s.whatsapp.net"},
			"whatsapp",
			"5511999@s.whatsapp.net",
			true,
		},
		{"bare JID is not an ID", []string{"5511999@s.whatsapp.net"}, "whatsapp", "5511999@s.whatsapp.net", false},
		{"empty sender", []string{"123"}, "telegram", "", false},
		{"empty list", nil, "telegram", "123", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsOwner(tc.allowFrom, tc.chType, tc.sender); got != tc.want {
				t.Fatalf("IsOwner(%q, %q, %q) = %v, want %v", tc.allowFrom, tc.chType, tc.sender, got, tc.want)
			}
		})
	}
}

func TestCanMutate_InternalAndChannelLookup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels = config.ChannelsConfig{
		"telegram": &config.Channel{Type: "telegram", AllowFrom: config.FlexibleStringSlice{"telegram:123"}},
		"bot2":     &config.Channel{Type: "telegram", AllowFrom: config.FlexibleStringSlice{"telegram:555"}},
	}
	for _, ch := range []string{"cli", "system", "subagent"} {
		if !CanMutate(cfg, ch, "anyone") {
			t.Fatalf("internal channel %q denied", ch)
		}
	}
	if !CanMutate(cfg, "telegram", "123") {
		t.Fatal("owner denied on telegram")
	}
	// The channel type, not the channel name, is matched against the platform.
	if !CanMutate(cfg, "bot2", "555") {
		t.Fatal("owner denied on a telegram channel with another name")
	}
	if CanMutate(cfg, "telegram", "555") {
		t.Fatal("another channel's owner allowed")
	}
	if CanMutate(cfg, "discord", "123") {
		t.Fatal("unknown channel allowed")
	}
	if CanMutate(nil, "telegram", "123") {
		t.Fatal("nil config allowed")
	}
}
