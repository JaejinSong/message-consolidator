package backlog

import (
	"context"
	"testing"
	"time"

	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
)

func initTestDB(t *testing.T) {
	t.Helper()
	store.ResetForTest()
	if err := store.InitDB(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
}

// insertOpenTask inserts a minimal open task row so HandleTaskState can exercise it.
func insertOpenTask(ctx context.Context, t *testing.T, email, source, room, sourceTS, assignee string, assignedAt time.Time) store.MessageID {
	t.Helper()
	res, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, source_ts, assignee, assigned_at, done, is_deleted, created_at)
		 VALUES (?, ?, ?, 'still open task', ?, ?, ?, 0, 0, datetime('now'))`,
		email, source, room, sourceTS, assignee, assignedAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("insertOpenTask: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return store.MessageID(id)
}

// TestHandleTaskState_ApplyDowngradesAssigneeResolveToCandidate verifies -apply never
// hard-closes even a RESOLVE from the task's own assignee: it writes a confirm-first
// candidate instead and leaves done=0.
func TestHandleTaskState_ApplyDowngradesAssigneeResolveToCandidate(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenTask(ctx, t, "u@x", "whatsapp", "room", "r1", "Me", time.Now().Add(-time.Hour))

	s := NewStore(&services.DefaultTaskStore{}, store.GetDB(), true, "whatsapp")
	s.CurrentTask = store.ConsolidatedMessage{ID: id, Assignee: "Me"}
	s.CurrentSender = "Me"

	item := store.TodoItem{State: "resolve", ID: &id}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Room: "room", Requester: "Me", OriginalText: "done", SourceTS: "r1"}

	if _, err := s.HandleTaskState(ctx, store.GetDB(), "u@x", item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}

	if s.Written() != 1 {
		t.Errorf("expected 1 candidate written under -apply, got %d", s.Written())
	}
	var done int
	var metadata string
	row := store.GetDB().QueryRowContext(ctx, `SELECT done, COALESCE(metadata, '') FROM messages WHERE id = ?`, int64(id))
	if err := row.Scan(&done, &metadata); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if done != 0 {
		t.Error("expected done=0: the tool must never hard-close, even for the assignee's own RESOLVE")
	}
	if metadata == "" {
		t.Error("expected a confirm-first candidate recorded in metadata")
	}
}

// TestHandleTaskState_FallsBackToRequesterWhenCurrentSenderUnset verifies the recorded
// speaker falls back to msg.Requester when a caller never set CurrentSender.
func TestHandleTaskState_FallsBackToRequesterWhenCurrentSenderUnset(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenTask(ctx, t, "u@x", "slack", "room", "r1", "Me", time.Now().Add(-time.Hour))

	s := NewStore(&services.DefaultTaskStore{}, store.GetDB(), false, "slack")
	s.CurrentTask = store.ConsolidatedMessage{ID: id, Assignee: "Me"}

	item := store.TodoItem{State: "resolve", ID: &id}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Room: "room", Requester: "Me", OriginalText: "done", SourceTS: "r1"}

	if _, err := s.HandleTaskState(ctx, store.GetDB(), "u@x", item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	rows := s.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 recorded row, got %d", len(rows))
	}
	if rows[0].Speaker != "Me" {
		t.Errorf("Speaker = %q, want fallback to msg.Requester %q", rows[0].Speaker, "Me")
	}
}

// TestHandleTaskState_ApplyRespectsDismissal verifies a previously dismissed source is not
// re-recorded even under -apply.
func TestHandleTaskState_ApplyRespectsDismissal(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenTask(ctx, t, "u@x", "whatsapp", "room", "r1", "Me", time.Now().Add(-time.Hour))
	if _, err := store.GetDB().ExecContext(ctx,
		`UPDATE messages SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.completion_dismissed_source', 'r1') WHERE id = ?`,
		int64(id)); err != nil {
		t.Fatalf("seed dismissed marker: %v", err)
	}

	s := NewStore(&services.DefaultTaskStore{}, store.GetDB(), true, "whatsapp")
	s.CurrentTask = store.ConsolidatedMessage{ID: id, Assignee: "Me", Metadata: []byte(`{"completion_dismissed_source":"r1"}`)}

	item := store.TodoItem{State: "resolve", ID: &id}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Room: "room", Requester: "Me", OriginalText: "done", SourceTS: "r1"}

	if _, err := s.HandleTaskState(ctx, store.GetDB(), "u@x", item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if s.Written() != 0 {
		t.Errorf("expected the dismissed source to stay suppressed, got written=%d", s.Written())
	}
}

// TestAddCompletionCandidate_RecordsSpeakerFromCurrentSender verifies the counterparty
// RESOLVE path records CurrentSender as the audit row's speaker.
func TestAddCompletionCandidate_RecordsSpeakerFromCurrentSender(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenTask(ctx, t, "u@x", "slack", "room", "r1", "Someone Else", time.Now().Add(-time.Hour))

	s := NewStore(&services.DefaultTaskStore{}, store.GetDB(), true, "slack")
	s.CurrentTask = store.ConsolidatedMessage{ID: id, Room: "room"}
	s.CurrentSender = "Counterparty"

	cand := store.CompletionCandidate{SourceLink: "r1", SourceText: "already done", Status: "pending"}
	if err := s.AddCompletionCandidate(ctx, store.GetDB(), "u@x", id, cand); err != nil {
		t.Fatalf("AddCompletionCandidate: %v", err)
	}
	if s.Written() != 1 {
		t.Errorf("expected 1 candidate written under -apply, got %d", s.Written())
	}
	rows := s.Rows()
	if len(rows) != 1 || rows[0].Speaker != "Counterparty" {
		t.Fatalf("expected 1 row with speaker %q, got %+v", "Counterparty", rows)
	}
}

// TestIsResolved verifies IsResolved reflects a RESOLVE-prefixed recorded verdict.
func TestIsResolved(t *testing.T) {
	s := NewStore(nil, nil, false, "whatsapp")
	s.record(store.MessageID(1), "room", "RESOLVE (candidate)", "Someone", "quote")
	if !s.IsResolved(store.MessageID(1)) {
		t.Error("expected task 1 to be resolved")
	}
	if s.IsResolved(store.MessageID(2)) {
		t.Error("expected task 2 to be unresolved")
	}
}

func TestPrintResults_EmptyIsHandled(t *testing.T) {
	s := NewStore(nil, nil, false, "whatsapp")
	s.PrintResults() // must not panic on an empty result set
}
