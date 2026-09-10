package services

import (
	"context"
	"encoding/json"
	"message-consolidator/store"
	"message-consolidator/types"
	"testing"
)

func TestResolveProposals(t *testing.T) {
	s := &TasksService{}
	email := "test@example.com"
	room := "General"

	active := []store.ConsolidatedMessage{
		{
			ID:       101,
			Room:     "General",
			Category: "TASK",
			Task:     "Share the meeting invite",
			Metadata: json.RawMessage(`{"affinity_group_id":"meeting"}`),
		},
	}

	rawItems := []store.TodoItem{
		{
			Task:     "Share the meeting invite link", // sim≥0.85 vs existing
			Category: "TASK",
			State:    "new", // AI thinks it's new
		},
		{
			Task:     "Buy coffee", // New
			Category: "TASK",
			State:    "new",
		},
	}

	results := s.ResolveProposals(context.Background(), email, room, rawItems, active)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First item: should be matched to 101
	if results[0].ID == nil || *results[0].ID != 101 {
		t.Errorf("expected first item to match ID 101, got %v", results[0].ID)
	}
	if results[0].State != "update" {
		t.Errorf("expected state 'update' for matched task, got %s", results[0].State)
	}

	// Second item: should be new
	if results[1].ID != nil {
		t.Errorf("expected second item ID to be nil, got %v", results[1].ID)
	}
	if results[1].State != "new" {
		t.Errorf("expected state 'new' for unmatched task, got %s", results[1].State)
	}
}

func TestResolveProposals_IDMatch(t *testing.T) {
	s := &TasksService{}
	email := "test@example.com"
	room := "biz-global-thailand"

	existingID := store.MessageID(5)
	active := []store.ConsolidatedMessage{
		{ID: existingID, Room: room, Category: "TASK", Task: "Attend Carabao online meeting"},
	}

	// AI explicitly sets id=5 from ExistingTasksJSON — text similarity is low.
	rawItems := []store.TodoItem{
		{ID: &existingID, Task: "Attend Carabao meeting with Yosep next week", Category: "TASK", State: "update"},
	}

	results := s.ResolveProposals(context.Background(), email, room, rawItems, active)

	if results[0].ID == nil || *results[0].ID != existingID {
		t.Errorf("expected ID-based match to %d, got %v", existingID, results[0].ID)
	}
	if results[0].State != "update" {
		t.Errorf("expected state 'update', got %q", results[0].State)
	}
}

func TestResolveProposals_AffinityBonus(t *testing.T) {
	s := &TasksService{}
	email := "test@example.com"
	room := "General"

	active := []store.ConsolidatedMessage{
		{
			ID:       201,
			Room:     "General",
			Category: "TASK",
			Task:     "Report review",
			Metadata: json.RawMessage(`{"affinity_group_id":"report_group"}`),
		},
	}

	// Affinity Group Bonus: Lower text similarity but shared group ID
	rawItems := []store.TodoItem{
		{
			Task:            "Report: finish draft",
			Category:        "TASK",
			AffinityGroupID: "report_group",
			State:           "new",
		},
	}

	results := s.ResolveProposals(context.Background(), email, room, rawItems, active)

	if results[0].ID == nil || *results[0].ID != 201 {
		t.Errorf("expected affinity group match to 201, got %v", results[0].ID)
	}
}

// TestResolveProposals_CrossThreadGuard verifies findMatch respects thread boundaries.
func TestResolveProposals_CrossThreadGuard(t *testing.T) {
	s := &TasksService{}
	email := "test@example.com"
	room := "biz-global-tech"

	active := []store.ConsolidatedMessage{
		{ID: 301, Room: room, Category: "TASK", Task: "Resolve Carabao issue by updating K8s agent", ThreadID: "T1"},
	}

	cases := []struct {
		name      string
		threadID  string
		wantMatch bool
	}{
		{"same thread high sim", "T1", true},
		{"different thread high sim", "T2", false},
		{"empty thread high sim", "", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			item := store.TodoItem{
				Task:     "Resolve Carabao issue by updating K8s agent to latest", // sim ≥ 0.85
				Category: "TASK",
				ThreadID: tc.threadID,
				State:    "new",
			}
			results := s.ResolveProposals(context.Background(), email, room, []store.TodoItem{item}, active)
			if tc.wantMatch {
				if results[0].ID == nil || *results[0].ID != 301 {
					t.Errorf("%s: expected match to 301, got %v", tc.name, results[0].ID)
				}
			} else {
				if results[0].ID != nil {
					t.Errorf("%s: expected no match (different thread), got ID=%v", tc.name, *results[0].ID)
				}
			}
		})
	}
}

// TestFindMatch_SkipsMergedCandidates guards the archival sentinel. Why: merging only
// flips category to 'merged' and never sets done/is_deleted, while
// GetActiveTasksForContext (store/queries/messages.sql) filters on done/is_deleted
// alone -- so an archived task stays in the candidate list and an AI-supplied ID could
// bind a proposal to a row the UI permanently hides, discarding the content silently.
func TestFindMatch_SkipsMergedCandidates(t *testing.T) {
	s := &TasksService{}
	const room = "biz-global-thailand"
	merged := store.ConsolidatedMessage{
		ID:       9001,
		Room:     room,
		Category: string(types.CategoryMerged),
		Task:     "Prepare for LPPSA tender opening",
		ThreadID: "T1",
	}

	t.Run("ai supplied id cannot bind to a merged task", func(t *testing.T) {
		id := store.MessageID(9001)
		item := store.TodoItem{
			ID:       &id,
			Category: "TASK",
			Task:     "Prepare for LPPSA tender opening",
			ThreadID: "T1",
		}
		if match := s.findMatch(room, item, []store.ConsolidatedMessage{merged}); match != nil {
			t.Errorf("bound to merged task %d (%q)", match.ID, match.Task)
		}
	})

	t.Run("fuzzy path cannot bind to a merged task", func(t *testing.T) {
		item := store.TodoItem{
			Category: string(types.CategoryMerged),
			Task:     "Prepare for LPPSA tender opening",
			ThreadID: "T1",
		}
		if match := s.findMatch(room, item, []store.ConsolidatedMessage{merged}); match != nil {
			t.Errorf("bound to merged task %d (%q)", match.ID, match.Task)
		}
	})
}
