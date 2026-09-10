package services

import (
	"testing"

	"message-consolidator/store"
	"message-consolidator/types"
)

// TestFindDuplicate_AdversarialTitles is the calibration gate for duplicate linking.
// Why: the first attempt at this (reverted in 3aa3f91) used an absolute shared-token
// count, which has zero discriminating power here -- "...quotation to Customer Alpha..."
// and "...to Customer Beta..." share 8 content tokens, exactly as many as a genuine
// rewrite. Every "must not link" case below is drawn from that failure or from real
// production titles, so this table is the thing that has to stay green.
func TestFindDuplicate_AdversarialTitles(t *testing.T) {
	const room = "biz-global-thailand"
	s := &TasksService{}

	mustLink := []struct {
		name     string
		existing string
		incoming string
	}{
		{
			// Rows 13230 / 13231: same decision, reworded, categories TASK vs QUERY.
			name:     "TGIA forum decision reworded",
			existing: "Decide on Stream's invitation to co-host booth and presentation at TGIA CIO Forum 2026",
			incoming: "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing and presentation)",
		},
		{
			// Why: a short rewrite must still link -- an absolute-count bar could not
			// reach 4 here, which is why the ratio is what gates.
			name:     "short title reworded",
			existing: "Review the deployment plan",
			incoming: "Review deployment plan for the release",
		},
	}
	for _, tc := range mustLink {
		t.Run("link/"+tc.name, func(t *testing.T) {
			active := []store.ConsolidatedMessage{{ID: 1, Room: room, Category: "TASK", Task: tc.existing}}
			item := store.TodoItem{Category: "QUERY", Task: tc.incoming}
			if got := s.findDuplicate(room, item, active); got == nil {
				ov := measureTitleOverlap(tc.incoming, tc.existing)
				t.Errorf("no link (shared=%d coverage=%.2f conflict=%v)",
					ov.shared, ov.coverage, hasEntityConflict(tc.incoming, tc.existing))
			}
		})
	}

	mustNotLink := []struct {
		name     string
		existing string
		incoming string
	}{
		{
			name:     "same template different customer",
			existing: "Send monitoring POC proposal and quotation to Customer Alpha for September 2026",
			incoming: "Send monitoring POC proposal and quotation to Customer Beta for September 2026",
		},
		{
			name:     "same template different partner",
			existing: "Please confirm attendance for the monitoring project review meeting with Jasindo team",
			incoming: "Please confirm attendance for the monitoring project review meeting with Unipost team",
		},
		{
			name:     "same template different project",
			existing: "Prepare and send the POC proposal and quotation for the Carabao monitoring project",
			incoming: "Prepare and send the POC proposal and quotation for the SAMCO monitoring project",
		},
		{
			name:     "same meeting different action",
			existing: "Attend Jasindo meeting with Ms Dini (Head of Application)",
			incoming: "Prepare materials for Jasindo meeting with Ms Dini",
		},
		{
			name:     "shared word quotation only",
			existing: "Prepare and send official proposal and quotation for DHAS App, DB, Server, Web monitoring",
			incoming: "Approve quotation via Unipost",
		},
		{
			name:     "shared partner name only",
			existing: "Attend the Stream meeting (Postponed this month)",
			incoming: "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing and presentation)",
		},
		{
			name:     "shared project prefix only",
			existing: "Provide PDRM POC timeline in Puspakom format",
			incoming: "Review PDRM POC document PDRM-Promox.docx",
		},
		{
			name:     "function words only",
			existing: "Notify team of Coronation Day holiday via email",
			incoming: "Convert opportunities to outcomes via pipeline development",
		},
		{
			name:     "shared vendor name only",
			existing: "Submit September 2026 contract approval request in Unipost by September 15",
			incoming: "Review and provide comments on the Unipost workflow document",
		},
	}
	for _, tc := range mustNotLink {
		t.Run("distinct/"+tc.name, func(t *testing.T) {
			active := []store.ConsolidatedMessage{{ID: 1, Room: room, Category: "TASK", Task: tc.existing}}
			item := store.TodoItem{Category: "QUERY", Task: tc.incoming}
			if got := s.findDuplicate(room, item, active); got != nil {
				ov := measureTitleOverlap(tc.incoming, tc.existing)
				t.Errorf("wrongly linked to %q (shared=%d coverage=%.2f)", got.Task, ov.shared, ov.coverage)
			}
		})
	}
}

// TestHasEntityConflict pins the veto rule itself: two titles naming different entities
// conflict, but a one-sided extra name does not -- that is usually the longer form of the
// same name ("TGIA Insurance CIO Forum" vs "TGIA CIO Forum").
func TestHasEntityConflict(t *testing.T) {
	cases := []struct {
		name     string
		a, b     string
		conflict bool
	}{
		{"different customers", "Bill Customer Alpha", "Bill Customer Beta", true},
		{"one sided longer name", "Join TGIA Insurance CIO Forum", "Join TGIA CIO Forum", false},
		{"no proper nouns", "Review the deployment plan", "Review deployment plan again", false},
		{"same proper noun", "Email Jasindo today", "Email Jasindo tomorrow", false},
		{"leading word ignored", "Jasindo review", "Unipost review", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasEntityConflict(tc.a, tc.b); got != tc.conflict {
				t.Errorf("hasEntityConflict(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.conflict)
			}
		})
	}
}

// TestFindDuplicate_SkipsIneligibleCandidates guards the cheap exclusions.
func TestFindDuplicate_SkipsIneligibleCandidates(t *testing.T) {
	const room = "biz-global-thailand"
	s := &TasksService{}
	const title = "Decide on Stream's invitation to co-host booth at TGIA CIO Forum 2026"
	item := store.TodoItem{Category: "QUERY", Task: title}

	t.Run("other room", func(t *testing.T) {
		active := []store.ConsolidatedMessage{{ID: 1, Room: "other-room", Category: "TASK", Task: title}}
		if got := s.findDuplicate(room, item, active); got != nil {
			t.Errorf("linked across rooms: %q", got.Task)
		}
	})
	t.Run("merged candidate", func(t *testing.T) {
		active := []store.ConsolidatedMessage{{ID: 1, Room: room, Category: string(types.CategoryMerged), Task: title}}
		if got := s.findDuplicate(room, item, active); got != nil {
			t.Errorf("linked to merged task: %q", got.Task)
		}
	})
	t.Run("conflicting thread", func(t *testing.T) {
		threaded := store.TodoItem{Category: "QUERY", Task: title, ThreadID: "T1"}
		active := []store.ConsolidatedMessage{{ID: 1, Room: room, Category: "TASK", Task: title, ThreadID: "T2"}}
		if got := s.findDuplicate(room, threaded, active); got != nil {
			t.Errorf("linked across threads: %q", got.Task)
		}
	})
	t.Run("empty incoming title", func(t *testing.T) {
		active := []store.ConsolidatedMessage{{ID: 1, Room: room, Category: "TASK", Task: title}}
		if got := s.findDuplicate(room, store.TodoItem{Category: "QUERY"}, active); got != nil {
			t.Errorf("linked an empty title: %q", got.Task)
		}
	})
}

// TestFindDuplicate_PicksBestCandidate guards against recency deciding the target.
// Why: GetActiveTasksForContext orders by assigned_at DESC, so a first-match rule would
// attach a duplicate to whichever qualifying task happens to be newest.
func TestFindDuplicate_PicksBestCandidate(t *testing.T) {
	const room = "biz-global-indonesia"
	s := &TasksService{}
	active := []store.ConsolidatedMessage{
		{ID: 999, Room: room, Category: "TASK", Task: "Review the deployment plan and the rollback steps"},
		{ID: 111, Room: room, Category: "TASK", Task: "Review the deployment plan"},
	}
	item := store.TodoItem{Category: "QUERY", Task: "Review the deployment plan"}
	got := s.findDuplicate(room, item, active)
	if got == nil {
		t.Fatal("no link")
	}
	if got.ID != 111 {
		t.Errorf("linked to %d (%q); want the closest match 111", got.ID, got.Task)
	}
}

// TestResolveProposalItem_DuplicateIsLinkOnly pins the safety property that made this
// path acceptable at all: it may attach a proposal to the task it restates, but it may
// never hard-close one. A false close on a fuzzy match is silent and unrecoverable.
func TestResolveProposalItem_DuplicateIsLinkOnly(t *testing.T) {
	const room = "biz-global-thailand"
	const existing = "Decide on Stream's invitation to co-host booth and presentation at TGIA CIO Forum 2026"
	const incoming = "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing and presentation)"
	s := &TasksService{}
	active := []store.ConsolidatedMessage{{ID: 13230, Room: room, Category: "TASK", Task: existing}}

	cases := []struct {
		name      string
		state     string
		isFromMe  bool
		wantState string
		wantID    bool
	}{
		{"new becomes update", "new", false, "update", true},
		{"update stays update", "update", false, "update", true},
		{"resolve becomes confirm-first candidate", "resolve", false, "resolve_candidate", true},
		{"self resolve still confirm-first", "resolve", true, "resolve_candidate", true},
		{"cancel is dropped", "cancel", false, "none", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := store.TodoItem{
				Category: "QUERY",
				Task:     incoming,
				State:    tc.state,
				IsFromMe: tc.isFromMe,
			}
			got := s.resolveProposalItem(room, item, active)
			if got.State != tc.wantState {
				t.Errorf("State = %q, want %q", got.State, tc.wantState)
			}
			if tc.wantID && (got.ID == nil || *got.ID != 13230) {
				t.Errorf("ID = %v, want 13230", got.ID)
			}
		})
	}
}

// TestResolveProposalItem_NoneStateUntouched guards the state guard on the new path.
func TestResolveProposalItem_NoneStateUntouched(t *testing.T) {
	const room = "biz-global-thailand"
	s := &TasksService{}
	active := []store.ConsolidatedMessage{{
		ID: 13230, Room: room, Category: "TASK",
		Task: "Decide on Stream's invitation to co-host booth at TGIA CIO Forum 2026",
	}}
	item := store.TodoItem{
		Category: "QUERY",
		State:    "none",
		Task:     "Decide on joining Stream at TGIA Insurance CIO Forum 2026 (booth sharing)",
	}
	got := s.resolveProposalItem(room, item, active)
	if got.State != "none" || got.ID != nil {
		t.Errorf("state=%q id=%v; want none with no id", got.State, got.ID)
	}
}
