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

// TestDuplicateTopicOverlap_ProductionTitles calibrates the duplicate threshold on real
// extracted titles. Why: the TGIA CIO Forum decision was extracted four times because
// findMatch required identical categories (the same request came back as TASK and as
// QUERY) and then leaned on Jaro-Winkler, which tasks_merge.go already documents as
// unable to separate a reworded title from a different topic (2026-09-10).
func TestDuplicateTopicOverlap_ProductionTitles(t *testing.T) {
	duplicates := []struct {
		name string
		a, b string
	}{
		{
			name: "TGIA forum decision reworded",
			a:    "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing and presentation)",
			b:    "Decide on Stream's invitation to co-host booth and presentation at TGIA CIO Forum 2026",
		},
		{
			name: "TGIA forum cost question vs decision",
			a:    "Determine cost and return for TGIA Insurance CIO Forum 2026 participation",
			b:    "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing and presentation)",
		},
	}
	for _, tc := range duplicates {
		t.Run("dup/"+tc.name, func(t *testing.T) {
			if got := duplicateTopicOverlap(tc.a, tc.b); got < minDuplicateOverlap {
				t.Errorf("overlap = %d, want >= %d (real duplicate must link)", got, minDuplicateOverlap)
			}
		})
	}

	distinct := []struct {
		name string
		a, b string
	}{
		{
			name: "shared word quotation only",
			a:    "Prepare and send official proposal and quotation for DHAS App, DB, Server, Web monitoring",
			b:    "Approve quotation via Unipost",
		},
		{
			name: "shared partner name Stream only",
			a:    "Attend the Stream meeting (Postponed this month)",
			b:    "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing and presentation)",
		},
		{
			name: "shared project prefix PDRM POC",
			a:    "Provide PDRM POC timeline in Puspakom format",
			b:    "Review PDRM POC document PDRM-Promox.docx",
		},
		{
			name: "same meeting different action",
			a:    "Attend Jasindo meeting with Ms Dini (Head of Application)",
			b:    "Prepare materials for Jasindo meeting with Ms Dini",
		},
		{
			name: "function words only",
			a:    "Notify team of Coronation Day holiday via email",
			b:    "Convert opportunities to outcomes via pipeline development",
		},
		{
			name: "shared Unipost only",
			a:    "Submit September 2026 contract approval request in Unipost by September 15",
			b:    "Review and provide comments on the Unipost workflow document",
		},
	}
	for _, tc := range distinct {
		t.Run("distinct/"+tc.name, func(t *testing.T) {
			if got := duplicateTopicOverlap(tc.a, tc.b); got >= minDuplicateOverlap {
				t.Errorf("overlap = %d, want < %d (distinct tasks must not link)", got, minDuplicateOverlap)
			}
		})
	}
}

// TestFindMatch_LinksDuplicateAcrossCategoryDisagreement is the regression for the
// four-way TGIA duplicate: same room, same topic, categories TASK vs QUERY.
func TestFindMatch_LinksDuplicateAcrossCategoryDisagreement(t *testing.T) {
	s := &TasksService{}
	active := []store.ConsolidatedMessage{{
		ID:       13230,
		Room:     "biz-global-thailand",
		Category: "TASK",
		Task:     "Decide on Stream's invitation to co-host booth and presentation at TGIA CIO Forum 2026",
	}}
	item := store.TodoItem{
		Category: "QUERY",
		Task:     "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing and presentation)",
	}
	match := s.findMatch("biz-global-thailand", item, active)
	if match == nil {
		t.Fatal("findMatch returned nil; want the existing TGIA task")
	}
	if match.ID != 13230 {
		t.Errorf("matched ID = %d, want 13230", match.ID)
	}
}

// TestFindMatch_KeepsDistinctTasksSeparate guards the other direction.
func TestFindMatch_KeepsDistinctTasksSeparate(t *testing.T) {
	s := &TasksService{}
	active := []store.ConsolidatedMessage{{
		ID:       11799,
		Room:     "biz-global-malaysia",
		Category: "TASK",
		Task:     "Prepare for LPPSA tender opening",
	}}
	item := store.TodoItem{
		Category: "QUERY",
		Task:     "Manage account control during the user's meeting in Korea",
	}
	if match := s.findMatch("biz-global-malaysia", item, active); match != nil {
		t.Errorf("findMatch linked unrelated tasks: %q", match.Task)
	}
}
