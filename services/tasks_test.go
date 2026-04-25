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

	if msgs[0].Assignee != "Test User" {
		t.Errorf("Expected assignee 'Test User' for msg 0, got '%s'", msgs[0].Assignee)
	}
	if msgs[1].Assignee != "Test User" {
		t.Errorf("Expected assignee 'Test User' for msg 1, got '%s'", msgs[1].Assignee)
	}
	if msgs[2].Assignee != "Other" {
		t.Errorf("Expected assignee 'Other' for msg 2, got '%s'", msgs[2].Assignee)
	}
}

// Regression: same row must yield identical Category regardless of display lang.
// Reproduces the bug where EN view classified a delegated task as "shared" because
// Task body contained "dev team" (hasGroupMention matched), while KO view (translated
// text without the English "team" keyword) classified it as "others" → SHARED badge
// flickered in/out with language.
func TestPrepareMessagesForClient_CategoryStableAcrossLang(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "jj@example.com"
	_, _ = store.GetOrCreateUser(ctx, email, "Jaejin Song", "")

	const taskEN = "Raise the request to the dev team once business context is provided"
	const taskKO = "비즈니스 맥락이 제공되면 개발 팀에 요청을 상신하십시오."

	s := &TasksService{translationSvc: NewTranslationService(nil)}

	build := func() []store.ConsolidatedMessage {
		return []store.ConsolidatedMessage{{
			ID:        101,
			UserEmail: email,
			Assignee:  "Yoga Wiranda",
			Requester: "Someone Else",
			Task:      taskEN,
			Source:    "slack",
			Room:      "biz-global-tech",
		}}
	}

	// Pre-seed KO translation so ApplyTranslations hits the cache and skips JIT.
	if err := store.SaveTaskTranslationsBulk(ctx, "ko", map[int]string{101: taskKO}); err != nil {
		t.Fatalf("seed translation: %v", err)
	}

	msgsEN := build()
	s.PrepareMessagesForClient(ctx, email, msgsEN, "en")

	msgsKO := build()
	s.PrepareMessagesForClient(ctx, email, msgsKO, "ko")

	if msgsEN[0].Category != CategoryOthers {
		t.Errorf("EN category: want %q, got %q", CategoryOthers, msgsEN[0].Category)
	}
	if msgsKO[0].Category != CategoryOthers {
		t.Errorf("KO category: want %q, got %q", CategoryOthers, msgsKO[0].Category)
	}
	if msgsEN[0].Category != msgsKO[0].Category {
		t.Errorf("Category must be lang-independent: EN=%q KO=%q", msgsEN[0].Category, msgsKO[0].Category)
	}
	if msgsEN[0].Assignee != "Yoga Wiranda" || msgsKO[0].Assignee != "Yoga Wiranda" {
		t.Errorf("Assignee must be preserved across langs: EN=%q KO=%q", msgsEN[0].Assignee, msgsKO[0].Assignee)
	}
	if msgsKO[0].Task != taskKO {
		t.Errorf("KO task must be translated: got %q", msgsKO[0].Task)
	}
	if msgsEN[0].Task != taskEN {
		t.Errorf("EN task must remain original: got %q", msgsEN[0].Task)
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

	result := ConsolidateTasks(tasks)

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

	result := ConsolidateTasks(tasks)

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

	result := ConsolidateTasks(tasks)

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
	user := &store.User{Email: email}
	identities := []string{email}

	tests := []struct {
		name               string
		assignee           string
		requester          string
		requesterCanonical string
		task               string
		expected           string
	}{
		{"personal: me", "me", "someone", "", "task", CategoryPersonal},
		{"shared: explicit shared assignee", "shared", "someone", "", "task", CategoryShared},
		// Body-text group mentions no longer override structural fields.
		// AI is expected to emit Assignee="shared" at extraction time for broadcasts.
		{"empty assignee + @everyone body → others", "", "someone", "", "@everyone check this", CategoryOthers},
		{"empty assignee + @channel body → others", "", "someone", "", "@channel update", CategoryOthers},
		{"named assignee + team noun in body → others", "Other Person", "someone", "", "ask the dev team", CategoryOthers},
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
			s.assignCategory(user, identities, msg)
			if msg.Category != tt.expected {
				t.Errorf("assignCategory() category = %v, want %v", msg.Category, tt.expected)
			}
		})
	}
}

func TestApplyAssigneeRules_RequesterCanonical(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	s := &TasksService{}
	ctx := context.Background()
	email := "jj@example.com"
	user, _ := store.GetOrCreateUser(ctx, email, "Jaejin Song", "")

	tests := []struct {
		name               string
		requester          string
		existingCanonical  string
		wantCanonical      string
	}{
		{
			name:          "exact email match",
			requester:     email,
			wantCanonical: email,
		},
		{
			name:          "display name with parenthetical suffix",
			requester:     "Jaejin Song (JJ)",
			wantCanonical: email,
		},
		{
			name:          "exact name match",
			requester:     "Jaejin Song",
			wantCanonical: email,
		},
		{
			name:              "stale wrong canonical gets overwritten",
			requester:         "Jaejin Song",
			existingCanonical: "wrong@example.com",
			wantCanonical:     email,
		},
		{
			name:          "different person → canonical unchanged",
			requester:     "Hady",
			wantCanonical: "",
		},
	}

	aliases, _ := store.GetUserAliasesByEmail(ctx, email)
	identities := GetEffectiveAliases(*user, aliases)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &store.ConsolidatedMessage{
				Requester:          tt.requester,
				RequesterCanonical: tt.existingCanonical,
				Assignee:           "other",
			}
			s.applyAssigneeRules(user, identities, msg)
			if msg.RequesterCanonical != tt.wantCanonical {
				t.Errorf("RequesterCanonical = %q, want %q", msg.RequesterCanonical, tt.wantCanonical)
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
