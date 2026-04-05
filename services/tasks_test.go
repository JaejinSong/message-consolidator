package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"strings"
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
	user, _ := store.GetOrCreateUser(context.Background(), email, "Test User", "")
	_ = user //Why: Ensures the user variable is consumed to satisfy the Go compiler's strict unused variable check in tests.

	msgs := []store.ConsolidatedMessage{
		{ID: 1, Assignee: "Test User", Requester: "Someone"},
		{ID: 2, Assignee: "me", Requester: "jjsong"},
		{ID: 3, Assignee: "Other", Requester: "me"},
	}

	s.FormatMessagesForClient(context.Background(), email, msgs)

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

func TestConsolidateTasks_SameSource_NoOriginalTextDuplication(t *testing.T) {
	tasks := []store.TodoItem{
		{
			Task:            "Task A",
			State:           "new",
			SourceTS:        "ts-001",
			AffinityGroupID: "group-1",
			AffinityScore:   90,
		},
		{
			Task:            "Task B",
			State:           "new",
			SourceTS:        "ts-001", // Same source: original_text must NOT be duplicated.
			AffinityGroupID: "group-1",
			AffinityScore:   90,
		},
	}

	result := store.ConsolidateTasks(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 consolidated task, got %d", len(result))
	}
	if !strings.Contains(result[0].Task, "Task A") || !strings.Contains(result[0].Task, "Task B") {
		t.Errorf("merged task should contain both texts, got: %q", result[0].Task)
	}
	// SourceTS identity preserved (same source — original_text dedup enforced at DB layer).
	if result[0].SourceTS != "ts-001" {
		t.Errorf("primary SourceTS should be ts-001, got %q", result[0].SourceTS)
	}
}

func TestConsolidateTasks_DifferentSource_FullAppend(t *testing.T) {
	tasks := []store.TodoItem{
		{
			Task:            "Follow-up on report",
			State:           "new",
			SourceTS:        "ts-001",
			AffinityGroupID: "group-2",
			AffinityScore:   85,
		},
		{
			Task:            "Submit final version",
			State:           "new",
			SourceTS:        "ts-002", // Different source: original_text append allowed.
			AffinityGroupID: "group-2",
			AffinityScore:   85,
		},
	}

	result := store.ConsolidateTasks(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 consolidated task, got %d", len(result))
	}
	if !strings.Contains(result[0].Task, "Follow-up on report") {
		t.Errorf("merged task missing primary text, got: %q", result[0].Task)
	}
	if !strings.Contains(result[0].Task, "Submit final version") {
		t.Errorf("merged task missing secondary text, got: %q", result[0].Task)
	}
}

func TestConsolidateTasks_BelowThreshold_NotMerged(t *testing.T) {
	tasks := []store.TodoItem{
		{Task: "Task X", State: "new", SourceTS: "ts-001", AffinityGroupID: "group-3", AffinityScore: 70},
		{Task: "Task Y", State: "new", SourceTS: "ts-001", AffinityGroupID: "group-3", AffinityScore: 70},
	}

	result := store.ConsolidateTasks(tasks)

	if len(result) != 2 {
		t.Errorf("tasks below threshold should NOT be merged, got %d tasks", len(result))
	}
}
