package scanner

import (
	"context"
	"testing"

	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

// TestSlackProposalThreadAnchor_PreventsCrossThreadRename reproduces the renamed-task
// incident: a Slack reply in thread B fuzzy-matched an open task anchored in thread A
// of the same room because raw.ThreadID was always empty for Slack (channels/slack.go
// only sets ReplyToID). With proposalThreadAnchor wired in, the candidate's ThreadID
// now anchors on the reply's own thread ts, so ResolveProposals' cross-thread guard
// (services/tasks_merge.go findMatch) rejects the match instead of renaming thread A's task.
func TestSlackProposalThreadAnchor_PreventsCrossThreadRename(t *testing.T) {
	t.Parallel()

	room := "biz-global-tech"
	active := []store.ConsolidatedMessage{
		{ID: 301, Room: room, Category: "TASK", Task: "Resolve Carabao issue by updating K8s agent", ThreadID: "100.000000"},
	}

	// A reply in a different thread (B) with high text similarity to the thread-A task.
	replyInThreadB := types.RawMessage{ID: "200.000005", ReplyToID: "200.000000"}
	msgMap := map[string]types.RawMessage{"200.000005": replyInThreadB}

	candidates := []store.TodoItem{
		{
			SourceTS: "200.000005",
			Task:     "Resolve Carabao issue by updating K8s agent to latest",
			Category: "TASK",
			State:    "new",
		},
	}

	slackAd := &slackAdapter{}
	for i := range candidates {
		if raw, ok := msgMap[candidates[i].SourceTS]; ok {
			candidates[i].ThreadID = resolveCandidateThreadID(slackAd, raw)
		}
	}
	if candidates[0].ThreadID != "200.000000" {
		t.Fatalf("expected candidate ThreadID anchored on thread B (200.000000), got %q", candidates[0].ThreadID)
	}

	s := &services.TasksService{}
	results := s.ResolveProposals(context.Background(), "user@example.com", room, candidates, active)

	if results[0].ID != nil {
		t.Errorf("expected no match across threads, got bound to task ID=%v", *results[0].ID)
	}
}
