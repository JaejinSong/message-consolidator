package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"strings"
	"testing"
	"time"
)

// Why: handleUpdate now wraps its multi-statement transition in runTaskTx. The auto-tx
// path (q=nil) must commit task text + subtasks + assignee + source_channels together —
// regressions to "first failing UPDATE leaves prior writes committed" would let this pass
// before the wrap and fail after. We pin the success path so any future split is caught.
func TestHandleUpdate_AutoTx_AppliesAllStatementsTogether(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	room := "TX-Atomic-Room"
	t1 := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	email, id := setupAssignedAtFixture(t, room, "Alice", t1)

	idVal := id
	item := store.TodoItem{
		ID:         &idVal,
		State:      "update",
		Task:       "Updated Task Title",
		AssignedTo: "Bob",
		Subtasks: []store.TodoSubtask{
			{Task: "sub-1", AssigneeName: "Bob"},
			{Task: "sub-2", AssigneeName: "Bob"},
		},
	}
	msg := store.ConsolidatedMessage{
		UserEmail:    email,
		Source:       "telegram",
		Room:         room,
		OriginalText: "appended chunk",
		AssignedAt:   time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
	}

	if _, err := HandleTaskState(context.Background(), nil, email, item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}

	var task, assignee, sourceChannels, subtasks string
	if err := store.GetDB().QueryRow(
		"SELECT task, COALESCE(assignee, ''), COALESCE(source_channels, ''), COALESCE(subtasks, '') FROM messages WHERE id = ?",
		id,
	).Scan(&task, &assignee, &sourceChannels, &subtasks); err != nil {
		t.Fatalf("read: %v", err)
	}

	if task != "Updated Task Title" {
		t.Errorf("task = %q, want %q", task, "Updated Task Title")
	}
	if assignee != "Bob" {
		t.Errorf("assignee = %q, want Bob", assignee)
	}
	if !strings.Contains(sourceChannels, "telegram") {
		t.Errorf("source_channels = %q, want to contain telegram", sourceChannels)
	}
	if !strings.Contains(subtasks, "sub-1") || !strings.Contains(subtasks, "sub-2") {
		t.Errorf("subtasks = %q, want to contain sub-1 and sub-2", subtasks)
	}
}

// Why: handleResolve previously dropped AppendOriginalText errors. Wrapped in a tx, both
// statements must commit atomically when q is nil (auto-tx). This pins the success path so
// the rollback-on-error semantics introduced by runTaskTx are observable.
func TestHandleResolve_AutoTx_AppliesBothMarkDoneAndAppend(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "resolve-tx@example.com"
	room := "Resolve-Room"
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted) VALUES (?, 'slack', ?, 'Some Task', 'orig', 'ts-rsv', 0, 0)",
		email, room,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := store.MessageID(id64)

	idVal := id
	item := store.TodoItem{ID: &idVal, Status: "resolve"}
	msg := store.ConsolidatedMessage{
		UserEmail:    email,
		Source:       "slack",
		Room:         room,
		OriginalText: "그건 끝났어요",
	}

	got, err := HandleTaskState(context.Background(), nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if got != id {
		t.Fatalf("HandleTaskState id = %d, want %d", got, id)
	}

	var done int
	var orig string
	if err := store.GetDB().QueryRow("SELECT done, COALESCE(original_text, '') FROM messages WHERE id = ?", id).Scan(&done, &orig); err != nil {
		t.Fatalf("read: %v", err)
	}
	if done != 1 {
		t.Errorf("done = %d, want 1", done)
	}
	if !strings.Contains(orig, "[Resolved:") || !strings.Contains(orig, "그건 끝났어요") {
		t.Errorf("original_text = %q, want both [Resolved: prefix and appended chunk", orig)
	}
}

// Why: validateTargetTask drops cross-room operations (existing == nil). Under runTaskTx
// the auto-tx must commit (no statements ran) and the caller must still receive id=0 with
// no error — same observable behavior as before the wrapping.
func TestHandleUpdate_CrossRoomDrop_NoMutation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "cross-room@example.com"
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted) VALUES (?, 'slack', 'OriginalRoom', 'T', 'o', 'ts-x', 0, 0)",
		email,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := store.MessageID(id64)

	idVal := id
	item := store.TodoItem{ID: &idVal, State: "update", Task: "Hijacked"}
	msg := store.ConsolidatedMessage{
		UserEmail:    email,
		Source:       "slack",
		Room:         "AttackerRoom",
		OriginalText: "cross-room write attempt",
	}

	got, err := HandleTaskState(context.Background(), nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if got != 0 {
		t.Errorf("returned id = %d, want 0 (cross-room drop)", got)
	}

	var task, room string
	if err := store.GetDB().QueryRow("SELECT task, room FROM messages WHERE id = ?", id).Scan(&task, &room); err != nil {
		t.Fatalf("read: %v", err)
	}
	if task != "T" || room != "OriginalRoom" {
		t.Errorf("task=%q room=%q, want unchanged (T, OriginalRoom)", task, room)
	}
}
