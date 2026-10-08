package channels

import (
	"context"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

type typingMockChannel struct {
	mockChannel
	typingStarted int
}

func (m *typingMockChannel) StartTyping(context.Context, string) (func(), error) {
	m.typingStarted++
	return func() {}, nil
}

type nopPlaceholderRecorder struct{}

func (nopPlaceholderRecorder) RecordPlaceholder(string, string, string)  {}
func (nopPlaceholderRecorder) RecordTypingStop(string, string, func())   {}
func (nopPlaceholderRecorder) RecordReactionUndo(string, string, func()) {}

// N15 (audit round 2): channel typing.enabled was accepted and never read --
// the indicator always fired. An explicit "enabled": false now turns it off;
// a config that does not mention typing keeps it on, as before.
func TestBaseChannel_TypingConfig(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name    string
		enabled *bool
		want    int
	}{
		{"absent keeps typing", nil, 1},
		{"enabled", &on, 1},
		{"disabled", &off, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgBus := bus.NewMessageBus()
			defer msgBus.Close()
			ch := &typingMockChannel{}
			ch.BaseChannel = *NewBaseChannel("test", nil, msgBus, nil,
				WithTyping(config.TypingConfig{Enabled: tc.enabled}))
			ch.SetOwner(ch)
			ch.SetPlaceholderRecorder(nopPlaceholderRecorder{})

			if err := ch.HandleMessageWithContext(context.Background(), "chat1", "hi", nil,
				bus.InboundContext{SenderID: "user1"}); err != nil {
				t.Fatal(err)
			}
			if ch.typingStarted != tc.want {
				t.Fatalf("typing started %d time(s), want %d", ch.typingStarted, tc.want)
			}
		})
	}
}
