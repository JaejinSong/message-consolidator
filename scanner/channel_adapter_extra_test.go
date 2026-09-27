package scanner

import (
	"message-consolidator/store"
	"message-consolidator/types"
	"testing"
)

func TestAdapterMentions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		adapter      ChannelAdapter
		m            types.RawMessage
		wantLen      int
		wantContains string
	}{
		{
			name:         "whatsapp returns MentionedNames",
			adapter:      whatsAppAdapter{},
			m:            types.RawMessage{MentionedNames: []string{"Alice", "Bob"}},
			wantLen:      2,
			wantContains: "Alice",
		},
		{
			name:    "whatsapp with empty MentionedNames returns empty slice",
			adapter: whatsAppAdapter{},
			m:       types.RawMessage{MentionedNames: []string{}},
			wantLen: 0,
		},
		{
			name:    "telegram returns nil (no mention metadata)",
			adapter: telegramAdapter{},
			m:       types.RawMessage{MentionedNames: []string{"Alice"}},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.adapter.Mentions(tt.m)
			if len(got) != tt.wantLen {
				t.Errorf("%s.Mentions() len = %d, want %d; got %v", tt.adapter.Source(), len(got), tt.wantLen, got)
			}
			if tt.wantContains != "" {
				found := false
				for _, n := range got {
					if n == tt.wantContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s.Mentions() result %v does not contain %q", tt.adapter.Source(), got, tt.wantContains)
				}
			}
		})
	}
}

func TestAdapterIsFromMe(t *testing.T) {
	t.Parallel()
	user := store.User{Name: "Jae", Email: "jae@example.com"}
	tests := []struct {
		name    string
		adapter ChannelAdapter
		m       types.RawMessage
		want    bool
	}{
		{"whatsapp explicit flag", whatsAppAdapter{}, types.RawMessage{IsFromMe: true}, true},
		{"whatsapp sender matches name case-insensitive", whatsAppAdapter{}, types.RawMessage{Sender: "JAE"}, true},
		{"whatsapp sender matches email", whatsAppAdapter{}, types.RawMessage{Sender: "jae@example.com"}, true},
		{"whatsapp counterparty", whatsAppAdapter{}, types.RawMessage{Sender: "someone"}, false},
		{"telegram explicit flag", telegramAdapter{}, types.RawMessage{IsFromMe: true}, true},
		{"telegram counterparty", telegramAdapter{}, types.RawMessage{Sender: "someone"}, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.adapter.IsFromMe(tt.m, user); got != tt.want {
				t.Errorf("%s.IsFromMe() = %v, want %v", tt.adapter.Source(), got, tt.want)
			}
		})
	}
}

// TestResolveCandidateIsFromMe covers the split between adapter.IsFromMe (used
// by saveChannelItem's category override) and the injection loop's resolved
// value (used by isTrustedResolve). Slack's own message must resolve true here
// while adapter.IsFromMe itself stays pinned false.
func TestResolveCandidateIsFromMe(t *testing.T) {
	t.Parallel()
	user := store.User{Name: "Jae", Email: "jae@example.com", SlackID: "U123"}
	slackAd := &slackAdapter{}

	tests := []struct {
		name    string
		adapter ChannelAdapter
		m       types.RawMessage
		want    bool
	}{
		{"slack own message resolves true via resolveTrustSource", slackAd, types.RawMessage{Sender: "U123"}, true},
		{"slack counterparty message resolves false", slackAd, types.RawMessage{Sender: "U999"}, false},
		{"whatsapp (no resolveTrustSource) falls back to adapter.IsFromMe", whatsAppAdapter{}, types.RawMessage{IsFromMe: true}, true},
		{"whatsapp counterparty falls back to adapter.IsFromMe", whatsAppAdapter{}, types.RawMessage{Sender: "someone"}, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveCandidateIsFromMe(tt.adapter, tt.m, user); got != tt.want {
				t.Errorf("resolveCandidateIsFromMe() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("slack adapter.IsFromMe itself stays pinned false for category override", func(t *testing.T) {
		t.Parallel()
		if got := slackAd.IsFromMe(types.RawMessage{Sender: "U123"}, user); got != false {
			t.Errorf("slackAdapter.IsFromMe() = %v, want false (category-override behavior unchanged)", got)
		}
	})
}

// TestResolveCandidateThreadID covers the fix for the renamed-Slack-task incident:
// Slack RawMessages never populate ThreadID (channels/slack.go sets only ReplyToID),
// so without proposalThreadAnchor the cross-thread guards in services/tasks_merge.go
// never fired. Slack must now anchor on slackThreadTS; other channels are unchanged.
func TestResolveCandidateThreadID(t *testing.T) {
	t.Parallel()
	slackAd := &slackAdapter{}

	tests := []struct {
		name    string
		adapter ChannelAdapter
		m       types.RawMessage
		want    string
	}{
		{"slack root message anchors on its own ts", slackAd, types.RawMessage{ID: "100.000000"}, "100.000000"},
		{"slack reply anchors on the parent thread ts", slackAd, types.RawMessage{ID: "100.000001", ReplyToID: "100.000000"}, "100.000000"},
		{"whatsapp (no proposalThreadAnchor) keeps raw.ThreadID unchanged", whatsAppAdapter{}, types.RawMessage{ThreadID: "wa-thread"}, "wa-thread"},
		{"whatsapp with empty ThreadID stays empty", whatsAppAdapter{}, types.RawMessage{}, ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveCandidateThreadID(tt.adapter, tt.m); got != tt.want {
				t.Errorf("resolveCandidateThreadID() = %q, want %q", got, tt.want)
			}
		})
	}
}
