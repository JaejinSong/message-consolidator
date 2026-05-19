package services

import (
	"context"
	"encoding/json"
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
	if err := store.SaveTaskTranslationsBulk(ctx, "ko", map[store.MessageID]string{101: taskKO}); err != nil {
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
		// Why: Priority 3 (맡긴) must use IsAssigneeMarkedAsMine like Priority 1 (받은)
		// so that self-origin tasks with display-name-only requester get categorized
		// as delegated instead of falling through to reference.
		{"requested: display-name-only requester in identities", "someone", "me@example.com (JJ)", "me@example.com (JJ)", "do this", CategoryRequested},
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
		name              string
		requester         string
		existingCanonical string
		wantCanonical     string
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
			// Why: view fallback — RequesterCanonical == raw Requester signals contacts JOIN miss.
			// Self-promotion must still fire in this case.
			name:              "self-promotion: canonical fallback to raw display name",
			requester:         "Jaejin Song (JJ)",
			existingCanonical: "Jaejin Song (JJ)",
			wantCanonical:     email,
		},
		{
			// Why: ε-guard — alias-based promotion only fires when canonical is empty;
			// a pre-existing canonical (even a stale/wrong one) is not overwritten.
			name:              "stale canonical not overwritten by alias match alone",
			requester:         "Jaejin Song",
			existingCanonical: "wrong@example.com",
			wantCanonical:     "wrong@example.com",
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

func TestShouldClearAssignee(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"기타 업무", true},
		{"기타업무", true},
		{"  Other Tasks ", true},
		{"OTHER TASKS", true},
		{"미지정", true},
		{"", false},
		{"shared", false},
		{"Hady", false},
	}
	for _, tt := range tests {
		if got := shouldClearAssignee(tt.in); got != tt.want {
			t.Errorf("shouldClearAssignee(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsAssigneeGeneric(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"me", true},
		{"Me", true},
		{"shared", false},
		{"Hady", false},
	}
	for _, tt := range tests {
		if got := isAssigneeGeneric(tt.in); got != tt.want {
			t.Errorf("isAssigneeGeneric(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestExtractToHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"missing header", "C: x@y.com\nS: subj", ""},
		{"with newline boundary", "T: alice@x.com\nC: bob@x.com\nS: hi", "alice@x.com"},
		{"no trailing newline → returns rest", "T: alice@x.com", "alice@x.com"},
		{"multi-recipient preserved", "T: a@x.com, b@x.com\nC:", "a@x.com, b@x.com"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractToHeader(tt.in); got != tt.want {
				t.Errorf("extractToHeader(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsMeInToHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		header, email string
		want          bool
	}{
		{"", "me@x.com", false},
		{"alice@x.com, me@x.com", "ME@X.COM", true},
		{"Alice@X.com", "alice@x.com", true},
		{"alice@x.com", "me@x.com", false},
	}
	for _, tt := range tests {
		if got := isMeInToHeader(tt.header, tt.email); got != tt.want {
			t.Errorf("isMeInToHeader(%q, %q) = %v, want %v", tt.header, tt.email, got, tt.want)
		}
	}
}

func TestBuildTranslateRequest(t *testing.T) {
	t.Parallel()
	t.Run("no subtasks → plain text", func(t *testing.T) {
		t.Parallel()
		req := BuildTranslateRequest(42, "do the thing", nil)
		if req.ID != 42 || req.Text != "do the thing" {
			t.Errorf("plain req = %+v, want id=42 text='do the thing'", req)
		}
	})
	t.Run("with subtasks → JSON payload", func(t *testing.T) {
		t.Parallel()
		req := BuildTranslateRequest(7, "main", []store.Subtask{{Task: "sub a"}, {Task: "sub b"}})
		if req.ID != 7 {
			t.Errorf("ID = %d, want 7", req.ID)
		}
		var p struct {
			T string   `json:"t"`
			S []string `json:"s"`
		}
		if err := json.Unmarshal([]byte(req.Text), &p); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		if p.T != "main" || len(p.S) != 2 || p.S[0] != "sub a" || p.S[1] != "sub b" {
			t.Errorf("payload = %+v", p)
		}
	})
}

func TestParseTranslatedText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		raw       string
		wantMain  string
		wantSubs  []string
	}{
		{"empty", "", "", nil},
		{"plain string passthrough", "hello world", "hello world", nil},
		{"malformed JSON falls back to raw", "{not json", "{not json", nil},
		{"JSON payload split", `{"t":"main","s":["a","b"]}`, "main", []string{"a", "b"}},
		{"JSON without subs", `{"t":"only"}`, "only", nil},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMain, gotSubs := parseTranslatedText(tt.raw)
			if gotMain != tt.wantMain {
				t.Errorf("main = %q, want %q", gotMain, tt.wantMain)
			}
			if len(gotSubs) != len(tt.wantSubs) {
				t.Fatalf("subs len = %d, want %d", len(gotSubs), len(tt.wantSubs))
			}
			for i, s := range tt.wantSubs {
				if gotSubs[i] != s {
					t.Errorf("subs[%d] = %q, want %q", i, gotSubs[i], s)
				}
			}
		})
	}
}

func TestHasAffinityMatch(t *testing.T) {
	t.Parallel()
	withMeta := func(groupID string) *store.ConsolidatedMessage {
		raw, _ := json.Marshal(map[string]string{"affinity_group_id": groupID})
		return &store.ConsolidatedMessage{Metadata: raw}
	}

	tests := []struct {
		name string
		msg  *store.ConsolidatedMessage
		item store.TodoItem
		sim  float64
		want bool
	}{
		{"no item group → false", withMeta("g1"), store.TodoItem{AffinityGroupID: ""}, 0.9, false},
		{"sim below 0.50 → false", withMeta("g1"), store.TodoItem{AffinityGroupID: "g1"}, 0.49, false},
		{"empty msg metadata → false", &store.ConsolidatedMessage{}, store.TodoItem{AffinityGroupID: "g1"}, 0.9, false},
		{"meta unmarshal error → false", &store.ConsolidatedMessage{Metadata: []byte("not json")}, store.TodoItem{AffinityGroupID: "g1"}, 0.9, false},
		{"groups mismatch → false", withMeta("g2"), store.TodoItem{AffinityGroupID: "g1"}, 0.9, false},
		{"groups match → true", withMeta("g1"), store.TodoItem{AffinityGroupID: "g1"}, 0.55, true},
		{"meta has empty group → false", withMeta(""), store.TodoItem{AffinityGroupID: "g1"}, 0.9, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasAffinityMatch(tt.msg, tt.item, tt.sim); got != tt.want {
				t.Errorf("hasAffinityMatch = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTasksService_GetMissingIDs(t *testing.T) {
	t.Parallel()
	all := []store.MessageID{1, 2, 3, 4}
	cached := map[store.MessageID]string{1: "x", 3: "y"}
	got := FilterMissingIDs(all, cached)
	want := []store.MessageID{2, 4}
	if len(got) != len(want) {
		t.Fatalf("missing IDs = %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("missing[%d] = %d, want %d", i, got[i], id)
		}
	}
}

func TestTasksService_MergeBatchResults(t *testing.T) {
	t.Parallel()
	s := &TasksService{}
	ids := []store.MessageID{1, 2, 3}
	cached := map[store.MessageID]string{1: "cached-1"}
	newTrans := map[store.MessageID]string{2: "new-2"} // id=3 missing on both sides

	got := s.mergeBatchResults(ids, cached, newTrans)
	if len(got) != 3 {
		t.Fatalf("results len = %d, want 3", len(got))
	}
	if !got[0].Success || got[0].TranslatedText != "cached-1" {
		t.Errorf("idx 0 = %+v, want cached success", got[0])
	}
	if !got[1].Success || got[1].TranslatedText != "new-2" {
		t.Errorf("idx 1 = %+v, want new success", got[1])
	}
	if got[2].Success || got[2].Error == "" {
		t.Errorf("idx 2 = %+v, want failure with error message", got[2])
	}
}

func TestTasksService_TruncateTitle(t *testing.T) {
	t.Parallel()
	s := &TasksService{}
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10x", 10, "exactly10x"},
		{"this is too long for limit", 10, "this is..."},
	}
	for _, tt := range tests {
		if got := s.truncateTitle(tt.in, tt.max); got != tt.want {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestNewTasksService(t *testing.T) {
	t.Parallel()
	svc := NewTasksService(nil, nil)
	if svc == nil {
		t.Error("NewTasksService returned nil")
	}
	if got := svc.GetTranslationService(); got != nil {
		t.Errorf("GetTranslationService = %v, want nil when none set", got)
	}
}

func TestSetEmbedder(t *testing.T) {
	t.Parallel()
	svc := NewTasksService(nil, nil)
	svc.SetEmbedder(nil) // nil embedder is a valid no-op
}
