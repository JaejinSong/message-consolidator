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
		{"shared", false},
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
		{"T: me@example.com\nC: \nS: Hello\nB:\nbody", true},
		{"T: other@example.com\nC: me@example.com\nS: Hello\nB:\nbody", false},
		{"T: dev@group.com\nC: \nS: Hello\nB:\nbody", false}, //Why: Validates that group emails where the user is just a recipient (not in 'To') are correctly excluded.
		{"T: Me@Example.Com\nC: \nS: Hello\nB:\nbody", true}, //Why: Ensures case-insensitive header matching for robustness across different email clients.
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

func TestIsTaskMatchedByAlias_GroupMentions(t *testing.T) {
	aliases := []string{"Song"}

	tests := []struct {
		task     string
		expected bool
	}{
		{"Project update for @everyone", false},
		{"Hello team, please check this", false},
		{"Task for Song", true},
		{"Everyone should do this", false},
		{"@channel check this", false},
	}

	for _, tt := range tests {
		m := store.ConsolidatedMessage{Task: tt.task, OriginalText: tt.task}
		if got := IsTaskMatchedByAlias(m, aliases, false); got != tt.expected {
			t.Errorf("IsTaskMatchedByAlias(%q) = %v; want %v", tt.task, got, tt.expected)
		}
	}
}

func TestAssignCategory(t *testing.T) {
	s := &TasksService{}
	email := "me@example.com"


	tests := []struct {
		name               string
		assignee           string
		requester          string
		requesterCanonical string
		task               string
		expected           string
	}{
		{"personal: me", "me", "someone", "", "task", CategoryPersonal},
		{"shared: shared", "shared", "someone", "", "task", CategoryShared},
		{"shared: group mention @everyone", "", "someone", "", "@everyone check this", CategoryShared},
		{"shared: group mention @channel", "", "someone", "", "@channel update", CategoryShared},
		{"shared: group mention @here", "", "someone", "", "@here heads up", CategoryShared},
		{"shared: group mention everyone keyword", "", "someone", "", "everyone please review", CategoryShared},
		{"shared: group mention team keyword", "", "someone", "", "team please check this", CategoryShared},
		{"requested: me to someone", "someone", email, "", "do this", CategoryRequested},
		{"requested: my canonical email to someone", "someone", "Jaejin Song", email, "do this", CategoryRequested},
		{"others: default", "someone", "someone", "", "just fyi", CategoryOthers},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &store.ConsolidatedMessage{
				Assignee:           tt.assignee,
				Requester:          tt.requester,
				RequesterCanonical: tt.requesterCanonical,
				Task:               tt.task,
			}
			s.assignCategory(email, msg)
			if msg.Category != tt.expected {
				t.Errorf("assignCategory() category = %v, want %v", msg.Category, tt.expected)
			}
		})
	}
}

func TestNormalizeRequesterMatching(t *testing.T) {
	tests := []struct {
		name      string
		requester string
		alias     string
		want      bool
	}{
		{"parenthesized suffix", "Jaejin Song (JJ)", "Jaejin Song", true},
		{"korean name", "송재진", "송재진", true},
		{"nickname only", "JJ", "JJ", true},
		{"email", "jjsong@whatap.io", "jjsong@whatap.io", true},
		{"different person", "Jane Doe (JD)", "Jaejin Song", false},
		{"case insensitive", "jaejin song (jj)", "Jaejin Song", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normReq := store.NormalizeIdentifier(tt.requester)
			normAlias := store.NormalizeIdentifier(tt.alias)
			got := strings.EqualFold(normReq, normAlias)
			if got != tt.want {
				t.Errorf("NormalizeIdentifier match(%q, %q) = %v, want %v", tt.requester, tt.alias, got, tt.want)
			}
		})
	}
}
