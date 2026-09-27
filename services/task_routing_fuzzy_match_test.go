package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"strings"
	"testing"
)

// Why: a fuzzy (title-similarity-only, no ID/thread anchor) match must append the new
// activity to original_text but never overwrite the existing task title -- the Slack
// wrong-task rename incident happened because a fuzzy match was allowed to rename an
// unrelated task via UpdateTaskFullAppend.
func TestHandleUpdate_FuzzyMatched_AppendsWithoutRenaming(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	room := "Fuzzy-Room"
	email := "fuzzy-match-test@example.com"
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted) VALUES (?, 'slack', ?, 'Arrange lunch', 'orig text', 'ts-1', 0, 0)",
		email, room,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := store.MessageID(id64)

	item := store.TodoItem{
		ID:           &id,
		State:        "update",
		Task:         "Arrange dinner with the Skyworx team",
		FuzzyMatched: true,
	}
	msg := store.ConsolidatedMessage{
		UserEmail:    email,
		Source:       "slack",
		Room:         room,
		OriginalText: "let's arrange dinner instead",
	}

	if _, err := HandleTaskState(context.Background(), nil, email, item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}

	var task, originalText string
	if err := store.GetDB().QueryRow(
		"SELECT task, COALESCE(original_text, '') FROM messages WHERE id = ?", id,
	).Scan(&task, &originalText); err != nil {
		t.Fatalf("read: %v", err)
	}

	if task != "Arrange lunch" {
		t.Errorf("task = %q, want unchanged %q (fuzzy match must not rename)", task, "Arrange lunch")
	}
	if !strings.Contains(originalText, "let's arrange dinner instead") {
		t.Errorf("original_text = %q, want to contain appended text", originalText)
	}
}
