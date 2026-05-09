package scanner

import (
	"message-consolidator/types"
	"testing"
)

func TestResolveAdapterMentions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		source       string
		m            types.RawMessage
		wantLen      int
		wantContains string
	}{
		{
			name:         "whatsapp returns MentionedNames",
			source:       "whatsapp",
			m:            types.RawMessage{MentionedNames: []string{"Alice", "Bob"}},
			wantLen:      2,
			wantContains: "Alice",
		},
		{
			name:    "whatsapp with empty MentionedNames returns empty slice",
			source:  "whatsapp",
			m:       types.RawMessage{MentionedNames: []string{}},
			wantLen: 0,
		},
		{
			name:    "telegram returns nil (no mention metadata)",
			source:  "telegram",
			m:       types.RawMessage{MentionedNames: []string{"Alice"}},
			wantLen: 0,
		},
		{
			name:    "slack returns nil (not whatsapp)",
			source:  "slack",
			m:       types.RawMessage{MentionedNames: []string{"Alice"}},
			wantLen: 0,
		},
		{
			name:    "empty source returns nil",
			source:  "",
			m:       types.RawMessage{MentionedNames: []string{"X"}},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveAdapterMentions(tt.source, tt.m)
			if len(got) != tt.wantLen {
				t.Errorf("resolveAdapterMentions(%q) len = %d, want %d; got %v", tt.source, len(got), tt.wantLen, got)
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
					t.Errorf("resolveAdapterMentions(%q) result %v does not contain %q", tt.source, got, tt.wantContains)
				}
			}
		})
	}
}

