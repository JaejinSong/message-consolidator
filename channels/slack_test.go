package channels

import (
	"testing"

	"github.com/slack-go/slack"
)

func TestSlackChannelDisplayName(t *testing.T) {
	cases := []struct {
		name     string
		channel  *slack.Channel
		fallback string
		want     string
	}{
		{
			name: "named public channel returns name",
			channel: &slack.Channel{
				GroupConversation: slack.GroupConversation{Name: "general"},
			},
			fallback: "C123",
			want:     "general",
		},
		{
			name: "empty IM returns DM label",
			channel: &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{IsIM: true},
				},
			},
			fallback: "D999",
			want:     "DM",
		},
		{
			name: "empty MpIM returns Group DM label",
			channel: &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{IsMpIM: true},
				},
			},
			fallback: "G999",
			want:     "Group DM",
		},
		{
			name:     "unknown empty channel returns id fallback",
			channel:  &slack.Channel{},
			fallback: "C000",
			want:     "C000",
		},
		{
			name: "named IM keeps the name",
			channel: &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Name:         "ignored",
					Conversation: slack.Conversation{IsIM: true},
				},
			},
			fallback: "D123",
			want:     "ignored",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := slackChannelDisplayName(c.channel, c.fallback)
			if got != c.want {
				t.Errorf("slackChannelDisplayName = %q, want %q", got, c.want)
			}
		})
	}
}
