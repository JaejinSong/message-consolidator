package services

import (
	"context"
	"encoding/json"
	"message-consolidator/store"
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
			Task:     "Please share meeting invite", // Similar
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
