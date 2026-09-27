package services

import (
	"context"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func TestIsEventTask(t *testing.T) {
	tests := []struct {
		title string
		want  bool
	}{
		{"Join Mesiniaga session at 11am with the vendor team", true},
		{"Schedule a brief XLSmart discussion with sunpho today", true},
		{"Attend BestChoice and Indomaret Korea visit", true},
		{"Prepare the POC review report", false},
		{"Share /data/rumctl/conf/config.yaml", false},
		{"Callback URL fix", false},
	}
	for _, tt := range tests {
		if got := isEventTask(tt.title); got != tt.want {
			t.Errorf("isEventTask(%q) = %v, want %v", tt.title, got, tt.want)
		}
	}
}

func seedPastEventServiceTask(t *testing.T, email, task, deadlineDate, metadata string) store.MessageID {
	t.Helper()
	src := testutil.RandomTS("pesvc")
	res, err := store.GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  requester, assignee, deadline_date, created_at, updated_at)
		 VALUES (?, ?, 'TASK', 'slack', 'general', ?, 0, 0, ?, ?, 'bob', ?, datetime('now'), datetime('now'))`,
		email, task, src, metadata, email, deadlineDate,
	)
	if err != nil {
		t.Fatalf("seedPastEventServiceTask: %v", err)
	}
	id, _ := res.LastInsertId()
	return store.MessageID(id)
}

func TestProposePastEventCandidates(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("pastevent-svc")
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	eventID := seedPastEventServiceTask(t, email, "Join Mesiniaga session at 11am", "2026-09-25", "{}")
	nonEventID := seedPastEventServiceTask(t, email, "Prepare the POC review report", "2026-09-25", "{}")
	dismissedID := seedPastEventServiceTask(t, email, "Attend BestChoice visit", "2026-09-20",
		`{"completion_dismissed_source":"past-event:2026-09-20"}`)

	svc := NewPastEventService()
	proposed, err := svc.ProposePastEventCandidates(ctx, email, now)
	if err != nil {
		t.Fatalf("ProposePastEventCandidates: %v", err)
	}
	if proposed != 1 {
		t.Fatalf("expected exactly 1 candidate proposed, got %d", proposed)
	}
	if meta := exclusionMeta(t, eventID); !store.HasPendingCompletionCandidate(meta) {
		t.Errorf("expected pending completion candidate written for event task, got %s", meta)
	}
	if meta := exclusionMeta(t, nonEventID); store.HasPendingCompletionCandidate(meta) {
		t.Errorf("expected non-event task to be skipped, got %s", meta)
	}
	if meta := exclusionMeta(t, dismissedID); store.HasPendingCompletionCandidate(meta) {
		t.Errorf("expected dismissed source key to stay skipped, got %s", meta)
	}
}
