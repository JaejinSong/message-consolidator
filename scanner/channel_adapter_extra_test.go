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
