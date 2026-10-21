package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"strings"
	"testing"
)

func TestMergeTasksWithTitle_RetriesWholeTxAfterLockError(t *testing.T) {
	// Arrange
	setupStoreTestDB(t)
	ctx := context.Background()
	email := testutil.RandomEmail("merge-lock")
	ids := make([]int64, 3)
	for i, task := range []string{"dest task", "src one", "src two"} {
		res, err := GetDB().Exec(
			"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted) VALUES (?, 'slack', 'Merge-Room', ?, ?, ?, 0, 0)",
			email, task, "text-"+task, testutil.RandomTS("m"))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		ids[i], _ = res.LastInsertId()
	}
	dest, srcs := MessageID(ids[0]), []MessageID{MessageID(ids[1]), MessageID(ids[2])}
	// Why: fires on the source-category update, after the dest row was already rewritten in the failed attempt.
	armLockOnceTrigger(t, `CREATE TRIGGER test_lock_merge BEFORE UPDATE OF category ON messages WHEN NEW.category = 'merged' BEGIN SELECT test_lock_once(); END`)

	// Act
	err := MergeTasksWithTitle(ctx, email, srcs, dest, "merged title")

	// Assert
	if err != nil {
		t.Fatalf("MergeTasksWithTitle: %v", err)
	}
	if got := lockOnceCalls.Load(); got != 1 {
		t.Fatalf("injected lock errors = %d, want 1", got)
	}
	var task, text string
	if err := GetDB().QueryRow("SELECT task, original_text FROM messages WHERE id = ?", ids[0]).Scan(&task, &text); err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if task != "merged title" {
		t.Errorf("dest task = %q, want %q", task, "merged title")
	}
	if n := strings.Count(text, "[Merge History]"); n != 1 {
		t.Errorf("merge history blocks = %d, want 1", n)
	}
	if n := strings.Count(text, "text-dest task"); n != 1 {
		t.Errorf("dest original text occurrences = %d, want 1", n)
	}
	for _, id := range ids[1:] {
		var cat string
		if err := GetDB().QueryRow("SELECT category FROM messages WHERE id = ?", id).Scan(&cat); err != nil {
			t.Fatalf("read src: %v", err)
		}
		if cat != "merged" {
			t.Errorf("source %d category = %q, want merged", id, cat)
		}
	}
}
