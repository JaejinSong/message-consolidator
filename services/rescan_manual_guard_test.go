package services

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

// Why: a manually-set assignee must survive an AI-driven rescan (principle 6) --
// seeds field_sources.assignee=manual, runs the "update" route, and asserts the
// assignee is unchanged even though the AI item carries a different AssignedTo.
func TestHandleTaskState_UpdateSkipsManualAssignee(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()

	email := testutil.RandomEmail("rescan-guard")
	room := "general"
	meta, err := MetadataSet(nil, "field_sources", map[string]string{"assignee": "manual"})
	if err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, source, room, task, assignee, original_text, source_ts, done, is_deleted, metadata)
		 VALUES (?, 'whatsapp', ?, 'Original task', 'alice', 'orig', ?, 0, 0, ?)`,
		email, room, testutil.RandomTS("guard"), string(meta),
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := store.MessageID(id64)

	idVal := id
	item := store.TodoItem{ID: &idVal, State: "update", Task: "Original task", AssignedTo: "bob"}
	msg := store.ConsolidatedMessage{UserEmail: email, Source: "whatsapp", Room: room, OriginalText: "new activity"}

	if _, err := HandleTaskState(context.Background(), nil, email, item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}

	var assignee string
	if err := store.GetDB().QueryRow("SELECT COALESCE(assignee, '') FROM messages WHERE id = ?", id64).Scan(&assignee); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if assignee != "alice" {
		t.Errorf("assignee = %q, want unchanged %q (manual field must survive rescan)", assignee, "alice")
	}
}

// Why: a manually-edited task title must survive an AI-driven rescan; the update
// route must fall back to append-only instead of overwriting the title.
func TestHandleTaskState_UpdateSkipsManualTaskTitle(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()

	email := testutil.RandomEmail("rescan-guard-task")
	room := "general"
	meta, err := MetadataSet(nil, "field_sources", map[string]string{"task": "manual"})
	if err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted, metadata)
		 VALUES (?, 'whatsapp', ?, 'Human-corrected title', 'orig', ?, 0, 0, ?)`,
		email, room, testutil.RandomTS("guard-task"), string(meta),
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := store.MessageID(id64)

	idVal := id
	item := store.TodoItem{ID: &idVal, State: "update", Task: "AI overwrite attempt"}
	msg := store.ConsolidatedMessage{UserEmail: email, Source: "whatsapp", Room: room, OriginalText: "new activity"}

	if _, err := HandleTaskState(context.Background(), nil, email, item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}

	var task, original string
	if err := store.GetDB().QueryRow("SELECT task, original_text FROM messages WHERE id = ?", id64).Scan(&task, &original); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if task != "Human-corrected title" {
		t.Errorf("task = %q, want unchanged %q (manual title must survive rescan)", task, "Human-corrected title")
	}
	if original == "orig" {
		t.Error("expected original_text to still receive the append-only audit trail")
	}
}
