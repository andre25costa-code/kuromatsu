package channels

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

func TestBaseChannelIsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowList []string
		senderID  string
		want      bool
	}{
		{
			name:      "empty allowlist rejects all",
			allowList: nil,
			senderID:  "anyone",
			want:      false,
		},
		{
			name:      "star allowlist allows all",
			allowList: []string{"*"},
			senderID:  "anyone",
			want:      true,
		},
		{
			name:      "compound sender matches numeric allowlist",
			allowList: []string{"123456"},
			senderID:  "123456|alice",
			want:      true,
		},
		{
			name:      "compound sender matches username allowlist",
			allowList: []string{"@alice"},
			senderID:  "123456|alice",
			want:      true,
		},
		{
			name:      "numeric sender matches legacy compound allowlist",
			allowList: []string{"123456|alice"},
			senderID:  "123456",
			want:      true,
		},
		{
			name:      "non matching sender is denied",
			allowList: []string{"123456"},
			senderID:  "654321|bob",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewBaseChannel("test", nil, nil, tt.allowList)
			if got := ch.IsAllowed(tt.senderID); got != tt.want {
				t.Fatalf("IsAllowed(%q) = %v, want %v", tt.senderID, got, tt.want)
			}
		})
	}
}

func TestShouldRespondInGroup(t *testing.T) {
	tests := []struct {
		name        string
		gt          config.GroupTriggerConfig
		isMentioned bool
		content     string
		wantRespond bool
		wantContent string
	}{
		{
			name:        "no config - permissive default",
			gt:          config.GroupTriggerConfig{},
			isMentioned: false,
			content:     "hello world",
			wantRespond: true,
			wantContent: "hello world",
		},
		{
			name:        "no config - mentioned",
			gt:          config.GroupTriggerConfig{},
			isMentioned: true,
			content:     "hello world",
			wantRespond: true,
			wantContent: "hello world",
		},
		{
			name:        "mention_only - not mentioned",
			gt:          config.GroupTriggerConfig{MentionOnly: true},
			isMentioned: false,
			content:     "hello world",
			wantRespond: false,
			wantContent: "hello world",
		},
		{
			name:        "mention_only - mentioned",
			gt:          config.GroupTriggerConfig{MentionOnly: true},
			isMentioned: true,
			content:     "hello world",
			wantRespond: true,
			wantContent: "hello world",
		},
		{
			name:        "prefix match",
			gt:          config.GroupTriggerConfig{Prefixes: []string{"/ask"}},
			isMentioned: false,
			content:     "/ask hello",
			wantRespond: true,
			wantContent: "hello",
		},
		{
			name:        "prefix no match - not mentioned",
			gt:          config.GroupTriggerConfig{Prefixes: []string{"/ask"}},
			isMentioned: false,
			content:     "hello world",
			wantRespond: false,
			wantContent: "hello world",
		},
		{
			name:        "prefix no match - but mentioned",
			gt:          config.GroupTriggerConfig{Prefixes: []string{"/ask"}},
			isMentioned: true,
			content:     "hello world",
			wantRespond: true,
			wantContent: "hello world",
		},
		{
			name:        "multiple prefixes - second matches",
			gt:          config.GroupTriggerConfig{Prefixes: []string{"/ask", "/bot"}},
			isMentioned: false,
			content:     "/bot help me",
			wantRespond: true,
			wantContent: "help me",
		},
		{
			name:        "mention_only with prefixes - mentioned overrides",
			gt:          config.GroupTriggerConfig{MentionOnly: true, Prefixes: []string{"/ask"}},
			isMentioned: true,
			content:     "hello",
			wantRespond: true,
			wantContent: "hello",
		},
		{
			name:        "mention_only with prefixes - not mentioned, no prefix",
			gt:          config.GroupTriggerConfig{MentionOnly: true, Prefixes: []string{"/ask"}},
			isMentioned: false,
			content:     "hello",
			wantRespond: false,
			wantContent: "hello",
		},
		{
			name:        "empty prefix in list is skipped",
			gt:          config.GroupTriggerConfig{Prefixes: []string{"", "/ask"}},
			isMentioned: false,
			content:     "/ask test",
			wantRespond: true,
			wantContent: "test",
		},
		{
			name:        "prefix strips leading whitespace after prefix",
			gt:          config.GroupTriggerConfig{Prefixes: []string{"/ask "}},
			isMentioned: false,
			content:     "/ask hello",
			wantRespond: true,
			wantContent: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewBaseChannel("test", nil, nil, []string{"*"}, WithGroupTrigger(tt.gt))
			gotRespond, gotContent := ch.ShouldRespondInGroup(tt.isMentioned, tt.content)
			if gotRespond != tt.wantRespond {
				t.Errorf("ShouldRespondInGroup() respond = %v, want %v", gotRespond, tt.wantRespond)
			}
			if gotContent != tt.wantContent {
				t.Errorf("ShouldRespondInGroup() content = %q, want %q", gotContent, tt.wantContent)
			}
		})
	}
}

func TestIsAllowedSender(t *testing.T) {
	tests := []struct {
		name      string
		allowList []string
		sender    bus.SenderInfo
		want      bool
	}{
		{
			name:      "empty allowlist rejects all",
			allowList: nil,
			sender:    bus.SenderInfo{PlatformID: "anyone"},
			want:      false,
		},
		{
			name:      "star allowlist allows all",
			allowList: []string{"*"},
			sender:    bus.SenderInfo{PlatformID: "anyone"},
			want:      true,
		},
		{
			name:      "numeric ID matches PlatformID",
			allowList: []string{"123456"},
			sender: bus.SenderInfo{
				Platform:    "telegram",
				PlatformID:  "123456",
				CanonicalID: "telegram:123456",
			},
			want: true,
		},
		{
			name:      "canonical format matches",
			allowList: []string{"telegram:123456"},
			sender: bus.SenderInfo{
				Platform:    "telegram",
				PlatformID:  "123456",
				CanonicalID: "telegram:123456",
			},
			want: true,
		},
		{
			name:      "canonical format wrong platform",
			allowList: []string{"discord:123456"},
			sender: bus.SenderInfo{
				Platform:    "telegram",
				PlatformID:  "123456",
				CanonicalID: "telegram:123456",
			},
			want: false,
		},
		{
			name:      "@username matches",
			allowList: []string{"@alice"},
			sender: bus.SenderInfo{
				Platform:    "telegram",
				PlatformID:  "123456",
				CanonicalID: "telegram:123456",
				Username:    "alice",
			},
			want: true,
		},
		{
			name:      "compound id|username matches by ID",
			allowList: []string{"123456|alice"},
			sender: bus.SenderInfo{
				Platform:    "telegram",
				PlatformID:  "123456",
				CanonicalID: "telegram:123456",
				Username:    "alice",
			},
			want: true,
		},
		{
			name:      "non matching sender denied",
			allowList: []string{"654321"},
			sender: bus.SenderInfo{
				Platform:    "telegram",
				PlatformID:  "123456",
				CanonicalID: "telegram:123456",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewBaseChannel("test", nil, nil, tt.allowList)
			if got := ch.IsAllowedSender(tt.sender); got != tt.want {
				t.Fatalf("IsAllowedSender(%+v) = %v, want %v", tt.sender, got, tt.want)
			}
		})
	}
}

func TestHandleInboundContext_PublishesNormalizedContext(t *testing.T) {
	tests := []struct {
		name       string
		inbound    bus.InboundContext
		wantChat   string
		wantSender string
	}{
		{
			name: "direct uses sender as peer",
			inbound: bus.InboundContext{
				Channel:   "test",
				ChatID:    "chat-1",
				ChatType:  "direct",
				SenderID:  "user-1",
				MessageID: "msg-1",
			},
			wantChat:   "chat-1",
			wantSender: "user-1",
		},
		{
			name: "group uses chat as peer",
			inbound: bus.InboundContext{
				Channel:   "test",
				ChatID:    "group-1",
				ChatType:  "group",
				SenderID:  "user-2",
				MessageID: "msg-2",
			},
			wantChat:   "group-1",
			wantSender: "user-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgBus := bus.NewMessageBus()
			defer msgBus.Close()

			ch := NewBaseChannel("test", nil, msgBus, []string{"*"})
			ch.HandleInboundContext(context.Background(), tt.inbound.ChatID, "hello", nil, tt.inbound)

			msg := <-msgBus.InboundChan()
			if msg.ChatID != tt.wantChat {
				t.Fatalf("ChatID = %q, want %q", msg.ChatID, tt.wantChat)
			}
			if msg.SenderID != tt.wantSender {
				t.Fatalf("SenderID = %q, want %q", msg.SenderID, tt.wantSender)
			}
			if msg.Context.ChatType != tt.inbound.ChatType {
				t.Fatalf("ChatType = %q, want %q", msg.Context.ChatType, tt.inbound.ChatType)
			}
		})
	}
}

func enableTestLog(t *testing.T) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "channels.log")
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logger.DisableFileLogging)
	return logPath
}

func TestHandleMessage_EmptyAllowListDropsMessage(t *testing.T) {
	msgBus := bus.NewMessageBus()
	defer msgBus.Close()

	ch := NewBaseChannel("test", nil, msgBus, nil)
	ch.HandleInboundContext(context.Background(), "c1", "hi", nil, bus.InboundContext{
		Channel: "test", ChatID: "c1", ChatType: "direct", SenderID: "u1",
	})

	select {
	case msg := <-msgBus.InboundChan():
		t.Fatalf("message published despite empty allow_from: %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHandleMessage_EmptyAllowListWarnsOncePerChannel(t *testing.T) {
	logPath := enableTestLog(t)

	msgBus := bus.NewMessageBus()
	defer msgBus.Close()
	ch := NewBaseChannel("warnchan", nil, msgBus, nil)
	for _, id := range []string{"u-first", "u-second"} {
		ch.HandleInboundContext(context.Background(), "c1", "hi", nil, bus.InboundContext{
			Channel: "warnchan", ChatID: "c1", ChatType: "direct", SenderID: id,
		})
	}
	logger.DisableFileLogging()

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(logged), "\n") {
		if strings.Contains(line, "message dropped: allow_from is empty") {
			n++
			if !strings.Contains(line, `"level":"warn"`) || !strings.Contains(line, "u-first") {
				t.Fatalf("unexpected warn line: %s", line)
			}
		}
	}
	if n != 1 {
		t.Fatalf("got %d drop warnings, want exactly 1:\n%s", n, logged)
	}
}

func TestNewBaseChannel_EmptyAllowListLogsError(t *testing.T) {
	logPath := enableTestLog(t)

	NewBaseChannel("errchan", nil, nil, nil)
	NewBaseChannel("pico", nil, nil, nil)
	NewBaseChannel("openchan", nil, nil, []string{"*"})
	logger.DisableFileLogging()

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(logged)
	for _, want := range []string{`"level":"error"`, "allow_from", "errchan", `set allow_from to your user ID`, "access is gated by the token"} {
		if !strings.Contains(s, want) {
			t.Fatalf("log missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "openchan") {
		t.Fatalf("explicit \"*\" channel must not log an error:\n%s", s)
	}
}
