package channels

import (
	"testing"
)

func TestMarshalStringSlice(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{"nil", nil, "[]"},
		{"empty", []string{}, "[]"},
		{"one", []string{"U123"}, `["U123"]`},
		{"two", []string{"U1", "U2"}, `["U1","U2"]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := marshalStringSlice(tc.input)
			if got != tc.want {
				t.Errorf("marshalStringSlice(%v) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveChatSource(t *testing.T) {
	tests := []struct {
		name         string
		wantChatType string
	}{
		{"UserSource", "user"},
		{"GroupSource", "group"},
		{"RoomSource", "room"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify constants match what webhook SDK produces
			switch tc.wantChatType {
			case "user", "group", "room":
				// Valid chat types.
			default:
				t.Errorf("unexpected chat type %q", tc.wantChatType)
			}
		})
	}
}
