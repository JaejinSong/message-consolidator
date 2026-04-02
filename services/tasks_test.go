package services

import (
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

func TestStripOriginalText(t *testing.T) {
	s := &TasksService{}
	msgs := []store.ConsolidatedMessage{
		{ID: 1, OriginalText: "Hello World"},
		{ID: 2, OriginalText: ""},
	}

	s.StripOriginalText(msgs)

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
	s := &TasksService{}
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
		if got := s.IsAssigneeMarkedAsMine(tt.assignee, identities); got != tt.expected {
			t.Errorf("IsAssigneeMarkedAsMine(%q) = %v; want %v", tt.assignee, got, tt.expected)
		}
	}
}

func TestFormatMessagesForClient(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	s := &TasksService{}
	email := "test@example.com"
	user, _ := store.GetOrCreateUser(email, "Test User", "")
	_ = user //Why: Ensures the user variable is consumed to satisfy the Go compiler's strict unused variable check in tests.

	msgs := []store.ConsolidatedMessage{
		{ID: 1, Assignee: "Test User", Requester: "Someone"},
		{ID: 2, Assignee: "me", Requester: "jjsong"},
		{ID: 3, Assignee: "Other", Requester: "me"},
	}

	s.FormatMessagesForClient(email, msgs)

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

func TestIsDirectlyAddressedToMe(t *testing.T) {
	s := &TasksService{}
	email := "me@example.com"

	tests := []struct {
		text     string
		expected bool
	}{
		{"To: me@example.com\nSubject: Hello", true},
		{"To: other@example.com\nCc: me@example.com\nSubject: Hello", false},
		{"To: dev@group.com\nSubject: Hello", false}, //Why: Validates that group emails where the user is just a recipient (not in 'To') are correctly excluded.
		{"to: me@example.com\nSubject: Hello", true},  //Why: Ensures case-insensitive header matching for robustness across different email clients.
	}

	for _, tt := range tests {
		m := store.ConsolidatedMessage{Source: "gmail", OriginalText: tt.text}
		if got := s.IsDirectlyAddressedToMe(m, email); got != tt.expected {
			t.Errorf("IsDirectlyAddressedToMe(%q) = %v; want %v", tt.text, got, tt.expected)
		}
	}
}
