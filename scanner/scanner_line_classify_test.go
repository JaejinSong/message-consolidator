package scanner

import (
	"testing"

	"message-consolidator/db"
	"message-consolidator/store"
	"message-consolidator/types"
)

func TestClassifyLineMessage_Broadcast(t *testing.T) {
	user := &store.User{Email: "alice@example.com", Name: "Alice"}

	for _, chatType := range []string{"group", "room"} {
		t.Run(chatType, func(t *testing.T) {
			row := db.LineInbox{ChatType: chatType, Text: "hello"}
			got := classifyLineMessage(row, user, nil)
			if got != types.CategoryTask {
				t.Errorf("chatType=%s: got %q, want CategoryTask", chatType, got)
			}
		})
	}
}

func TestClassifyLineMessage_AliasMatch(t *testing.T) {
	user := &store.User{Email: "alice@example.com", Name: "Alice"}
	aliases := []string{"ali"}

	row := db.LineInbox{ChatType: "user", Text: "Hey Ali, please review this"}
	got := classifyLineMessage(row, user, aliases)
	if got != types.CategoryTask {
		t.Errorf("got %q, want CategoryTask", got)
	}
}

func TestClassifyLineMessage_NameMatch(t *testing.T) {
	user := &store.User{Email: "alice@example.com", Name: "Alice"}

	row := db.LineInbox{ChatType: "user", Text: "alice can you check this?"}
	got := classifyLineMessage(row, user, nil)
	if got != types.CategoryTask {
		t.Errorf("got %q, want CategoryTask", got)
	}
}

func TestClassifyLineMessage_NoMatch(t *testing.T) {
	user := &store.User{Email: "alice@example.com", Name: "Alice"}

	row := db.LineInbox{ChatType: "user", Text: "please handle the deployment"}
	got := classifyLineMessage(row, user, nil)
	if got != types.CategoryQuery {
		t.Errorf("got %q, want CategoryQuery", got)
	}
}

func TestIsLineBroadcast(t *testing.T) {
	tests := []struct {
		chatType string
		want     bool
	}{
		{"group", true},
		{"room", true},
		{"user", false},
		{"unknown", false},
	}
	for _, tc := range tests {
		t.Run(tc.chatType, func(t *testing.T) {
			row := db.LineInbox{ChatType: tc.chatType}
			got := isLineBroadcast(row)
			if got != tc.want {
				t.Errorf("isLineBroadcast(%q) = %v; want %v", tc.chatType, got, tc.want)
			}
		})
	}
}

func TestHasLineAliasMatch(t *testing.T) {
	user := &store.User{Name: "Bob"}

	tests := []struct {
		text    string
		aliases []string
		want    bool
	}{
		{"Bob please do this", nil, true},
		{"bob please do this", nil, true},
		{"completely unrelated", nil, false},
		{"hey bob-alias check", []string{"bob-alias"}, true},
		{"no match here", []string{"carol"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			got := hasLineAliasMatch(tc.text, user, tc.aliases)
			if got != tc.want {
				t.Errorf("hasLineAliasMatch(%q) = %v; want %v", tc.text, got, tc.want)
			}
		})
	}
}
