package services

import (
	"message-consolidator/store"
	"testing"
)

func TestStripOriginalText(t *testing.T) {
	msgs := []store.ConsolidatedMessage{
		{ID: 1, OriginalText: "Hello World"},
		{ID: 2, OriginalText: ""},
	}

	StripOriginalText(msgs)

	if !msgs[0].HasOriginal {
		t.Error("Expected HasOriginal to be true for msg 1")
	}
	if msgs[0].OriginalText != "" {
		t.Error("Expected OriginalText to be stripped for msg 1")
	}

	if msgs[1].HasOriginal {
		t.Error("Expected HasOriginal to be false for msg 2")
	}
}

func TestIsAssigneeMarkedAsMine(t *testing.T) {
	identities := []string{"Jaejin Song", "jjsong"}
	
	tests := []struct {
		assignee string
		expected bool
	}{
		{"me", true},
		{"Me", true},
		{"Jaejin Song", true},
		{"jjsong", true},
		{"Other", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsAssigneeMarkedAsMine(tt.assignee, identities); got != tt.expected {
			t.Errorf("IsAssigneeMarkedAsMine(%q) = %v; want %v", tt.assignee, got, tt.expected)
		}
	}
}

func TestFormatMessagesForClient(t *testing.T) {
	cleanup, err := store.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	user, _ := store.GetOrCreateUser(email, "Test User", "")
	_ = user // Ensure user is used to avoid build error

	msgs := []store.ConsolidatedMessage{
		{ID: 1, Assignee: "Test User", Requester: "Someone"},
		{ID: 2, Assignee: "me", Requester: "jjsong"},
		{ID: 3, Assignee: "Other", Requester: "me"},
	}

	FormatMessagesForClient(email, msgs)

	if msgs[0].Assignee != "me" {
		t.Errorf("Expected assignee 'me' for msg 0, got '%s'", msgs[0].Assignee)
	}
	if msgs[1].Assignee != "me" {
		t.Errorf("Expected assignee 'me' for msg 1, got '%s'", msgs[1].Assignee)
	}
	if msgs[2].Assignee != "Other" {
		t.Errorf("Expected assignee 'Other' for msg 2, got '%s'", msgs[2].Assignee)
	}
}
