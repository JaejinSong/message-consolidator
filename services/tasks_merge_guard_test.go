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

	if got := svc.findMatch(skyworxRoom, item, active); got != nil {
		t.Errorf("off-topic AI-supplied ID must be rejected, matched task %d", got.ID)
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

	got := svc.findMatch(skyworxRoom, item, active)
	if got == nil || got.ID != 12719 {
		t.Fatalf("rephrased-title update must keep its AI-supplied ID match, got %v", got)
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

	got := svc.findMatch(skyworxRoom, item, active)
	if got == nil || got.ID != 12719 {
		t.Fatalf("thread-anchored ID must be trusted, got %v", got)
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

	got := svc.findMatch(skyworxRoom, item, active)
	if got == nil || got.ID != 12719 {
		t.Fatalf("bare resolve with AI-supplied ID must match, got %v", got)
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
