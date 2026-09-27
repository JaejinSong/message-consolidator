package services

import (
	"testing"

	"message-consolidator/store"
)

const skyworxRoom = "[Technical] Skyworx x WhaTap"

func samcoTask(id store.MessageID) store.ConsolidatedMessage {
	return store.ConsolidatedMessage{
		ID:       id,
		Room:     skyworxRoom,
		Task:     "Implement APM in SAMCO environment via help from Skyworx team",
		Category: "TASK",
		ThreadID: "3CF634CD4FE7E7B34330",
	}
}

// Why: Indofood-PO regression (task 12719, 2026-07-15) — the AI bound an unrelated
// PO-sharing message to the SAMCO task's ID and the unverified ID-first path appended
// and later resolved it. An off-topic proposal carrying a valid ID must be rejected.
func TestFindMatch_RejectsOffTopicAIID(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{samcoTask(12719)}
	id := store.MessageID(12719)
	item := store.TodoItem{
		ID:       &id,
		State:    "update",
		Task:     "Share Indofood PO with Skyworx",
		Category: "TASK",
	}

	if got, fuzzy := svc.findMatch(skyworxRoom, item, active); got != nil || fuzzy {
		t.Errorf("off-topic AI-supplied ID must be rejected, matched task %v (fuzzy=%v)", got, fuzzy)
	}
}

// Why: a legitimately rephrased title shares topical tokens with the original —
// the ID verification must not reject genuine scope-refinement updates.
func TestFindMatch_AcceptsRephrasedTitleID(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{samcoTask(12719)}
	id := store.MessageID(12719)
	item := store.TodoItem{
		ID:       &id,
		State:    "update",
		Task:     "Implement APM in remaining SAMCO microservices",
		Category: "TASK",
	}

	got, fuzzy := svc.findMatch(skyworxRoom, item, active)
	if got == nil || got.ID != 12719 {
		t.Fatalf("rephrased-title update must keep its AI-supplied ID match, got %v", got)
	}
	if fuzzy {
		t.Errorf("AI-ID-verified match must not be flagged fuzzy")
	}
}

// Why: a quote-reply in the task's own thread is the strongest anchor — it must be
// trusted even when the proposal title diverges completely.
func TestFindMatch_AcceptsThreadAnchoredID(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{samcoTask(12719)}
	id := store.MessageID(12719)
	item := store.TodoItem{
		ID:       &id,
		State:    "resolve",
		Task:     "Totally different phrasing",
		Category: "TASK",
		ThreadID: "3CF634CD4FE7E7B34330",
	}

	got, fuzzy := svc.findMatch(skyworxRoom, item, active)
	if got == nil || got.ID != 12719 {
		t.Fatalf("thread-anchored ID must be trusted, got %v", got)
	}
	if fuzzy {
		t.Errorf("AI-ID-verified match must not be flagged fuzzy")
	}
}

// Why: bare resolve/cancel proposals carry no title to verify against; rejecting them
// would drop every ID-only resolve the model emits.
func TestFindMatch_AcceptsBareResolveID(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{samcoTask(12719)}
	id := store.MessageID(12719)
	item := store.TodoItem{ID: &id, State: "resolve", Category: "TASK"}

	got, fuzzy := svc.findMatch(skyworxRoom, item, active)
	if got == nil || got.ID != 12719 {
		t.Fatalf("bare resolve with AI-supplied ID must match, got %v", got)
	}
	if fuzzy {
		t.Errorf("AI-ID-verified match must not be flagged fuzzy")
	}
}

// Why: task 12761 scenario — a counterparty message (not fromMe, not in the task's
// reply chain) proposing resolve must be demoted to a confirm-first candidate instead
// of hard-closing the task.
func TestResolveProposalItem_DemotesUntrustedResolve(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{{
		ID:       12761,
		Room:     skyworxRoom,
		Task:     "Schedule SAMCO discussion this week",
		Category: "TASK",
		ThreadID: "3C967B9AD247FD9C4054",
	}}
	id := store.MessageID(12761)
	item := store.TodoItem{
		ID:       &id,
		State:    "resolve",
		Task:     "Schedule SAMCO discussion this week",
		Category: "TASK",
		IsFromMe: false,
	}

	got := svc.resolveProposalItem(skyworxRoom, item, active)
	if got.State != "resolve_candidate" {
		t.Errorf("counterparty resolve state = %q, want resolve_candidate", got.State)
	}
	if got.ID == nil || *got.ID != 12761 {
		t.Errorf("demoted resolve must keep its matched ID, got %v", got.ID)
	}
}

// Why: the user's own statement and in-thread replies remain trusted auto-close paths —
// demotion must not regress them.
func TestResolveProposalItem_TrustedResolveStaysResolve(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{{
		ID:       5,
		Room:     skyworxRoom,
		Task:     "Schedule SAMCO discussion this week",
		Category: "TASK",
		ThreadID: "THREAD-A",
	}}
	id := store.MessageID(5)

	cases := []struct {
		name string
		item store.TodoItem
	}{
		{"fromMe", store.TodoItem{ID: &id, State: "resolve", Task: "Schedule SAMCO discussion this week", Category: "TASK", IsFromMe: true}},
		{"same thread", store.TodoItem{ID: &id, State: "resolve", Task: "Schedule SAMCO discussion this week", Category: "TASK", ThreadID: "THREAD-A"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := svc.resolveProposalItem(skyworxRoom, tc.item, active)
			if got.State != "resolve" {
				t.Errorf("trusted resolve state = %q, want resolve", got.State)
			}
		})
	}
}

// Why: task 13304/13269 regression — a WhatsApp group's assignee rarely quote-replies,
// so the sender-is-assignee check is the only path that can hard-close their own report
// of completion instead of demoting it to resolve_candidate.
func TestResolveProposalItem_AssigneeOwnReport(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	id := store.MessageID(1)

	cases := []struct {
		name      string
		assignee  string
		sender    string
		threadID  string
		wantState string
	}{
		{"ambiguous marker matches plain name", "Andy Phan", "Andy Phan (Ambiguous)", "", "resolve"},
		{"case-insensitive match", "Ardi", "ardi", "", "resolve"},
		{"different sender stays candidate", "Ardi", "Faisal", "", "resolve_candidate"},
		{"shared assignee never trusted", "shared", "shared", "", "resolve_candidate"},
		{"empty sender never trusted", "Ardi", "", "", "resolve_candidate"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			active := []store.ConsolidatedMessage{{
				ID:       1,
				Room:     skyworxRoom,
				Task:     "Clarify per-core license CPU/vCPU counting scope",
				Category: "TASK",
				Assignee: tc.assignee,
				ThreadID: "THREAD-DIFFERENT",
			}}
			item := store.TodoItem{
				ID:         &id,
				State:      "resolve",
				Task:       "Clarify per-core license CPU/vCPU counting scope",
				Category:   "TASK",
				SenderName: tc.sender,
				ThreadID:   tc.threadID,
			}

			got := svc.resolveProposalItem(skyworxRoom, item, active)
			if got.State != tc.wantState {
				t.Errorf("state = %q, want %q", got.State, tc.wantState)
			}
		})
	}
}

// Why: a rejected AI-supplied ID must return no match at all -- previously it fell
// through to the fuzzy loop and could still bind to an unrelated task purely on
// similarity (the Slack "Arrange dinner" vs "Arrange lunch" incident).
func TestFindMatch_RejectedAIID_DoesNotFallThroughToFuzzy(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	// Two active tasks in the same room: the one the AI (wrongly) points its ID at, and
	// a second, topically-unrelated task whose title happens to be near-identical
	// (>=0.85 similarity, >=2 shared tokens) to the proposal's title -- exactly the shape
	// of the production incident, where the fuzzy loop re-matched onto the wrong task
	// after the ID was rejected.
	lunchTask := store.ConsolidatedMessage{
		ID:       800,
		Room:     skyworxRoom,
		Task:     "Arrange lunch",
		Category: "TASK",
	}
	active := []store.ConsolidatedMessage{samcoTask(12719), lunchTask}
	id := store.MessageID(12719)
	item := store.TodoItem{
		// AI points at the SAMCO task (12719), but the proposal title is topically
		// unrelated to it (no shared tokens) -- verifiedIDMatch rejects the ID. The
		// title is instead near-identical to the unrelated lunchTask.
		ID:       &id,
		State:    "update",
		Task:     "Arrange dinner with the whole team tonight",
		Category: "TASK",
	}

	got, fuzzy := svc.findMatch(skyworxRoom, item, active)
	if got != nil {
		t.Errorf("rejected AI-supplied ID must not fuzzy-fall-through, got match %d (%q)", got.ID, got.Task)
	}
	if fuzzy {
		t.Errorf("no match found, fuzzy must be false")
	}
}

// Why: prefix-shared-but-topically-different titles score high on Jaro-Winkler alone
// (measured: "Arrange dinner with the team" vs "Arrange lunch" = 0.966) -- the fuzzy
// loop must also require topical token overlap so it can't bind to the wrong task.
func TestFindMatch_FuzzyLoop_RequiresTopicalOverlap(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{{
		ID:       555,
		Room:     skyworxRoom,
		Task:     "Arrange lunch",
		Category: "TASK",
	}}
	item := store.TodoItem{
		State:    "new",
		Task:     "Arrange dinner with the whole team tonight",
		Category: "TASK",
	}

	if got, _ := svc.findMatch(skyworxRoom, item, active); got != nil {
		t.Errorf("prefix-only similar title with <2 shared tokens must not match, got %d", got.ID)
	}
}

// Why: the fuzzy loop's match must be flagged FuzzyMatched (and never IDVerified) so
// downstream update handling appends instead of renaming.
func TestFindMatch_FuzzyLoop_FlagsFuzzyMatch(t *testing.T) {
	t.Parallel()
	svc := &TasksService{}
	active := []store.ConsolidatedMessage{{
		ID:       556,
		Room:     skyworxRoom,
		Task:     "Implement APM in SAMCO environment via help from Skyworx team",
		Category: "TASK",
	}}
	item := store.TodoItem{
		State:    "new",
		Task:     "Implement APM in remaining SAMCO microservices",
		Category: "TASK",
	}

	got, fuzzy := svc.findMatch(skyworxRoom, item, active)
	if got == nil || got.ID != 556 {
		t.Fatalf("expected topically-overlapping high-similarity title to match, got %v", got)
	}
	if !fuzzy {
		t.Errorf("expected fuzzy=true for a match with no AI-supplied ID")
	}
}

func TestTitleTokenOverlap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b string
		want int
	}{
		{"off-topic", "Share Indofood PO with Skyworx", "Implement APM in SAMCO environment via help from Skyworx team", 1},
		{"rephrased", "Implement APM in remaining SAMCO microservices", "Implement APM in SAMCO environment via help from Skyworx team", 3},
		{"license vs scheduling", "Inject license into the server", "Schedule meeting with Netciti team", 0},
		{"empty", "", "Schedule meeting", 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := titleTokenOverlap(tc.a, tc.b); got != tc.want {
				t.Errorf("titleTokenOverlap(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
