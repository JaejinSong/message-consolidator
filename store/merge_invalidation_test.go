package store

import (
	"message-consolidator/internal/testutil"
	"context"
	"testing"
)

// Why: Regression test to ensure that when tasks are merged, their translation cache is invalidated.
// This prevents the UI from showing stale translations for the new merged content.
func TestMergeTaskInvalidation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("merge-inv")

	// 1. Setup: Create destination task and a source task
	res1, err := GetDB().Exec("INSERT INTO messages (user_email, task, source, source_ts, pinned, done, is_deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		email, "Source Task", "slack", "ts-merge-1", false, 0, 0)
	if err != nil { t.Fatalf("seed error: %v", err) }
	id1, _ := res1.LastInsertId()

	res2, err := GetDB().Exec("INSERT INTO messages (user_email, task, source, source_ts, pinned, done, is_deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		email, "Dest Task", "slack", "ts-merge-2", false, 0, 0)
	if err != nil { t.Fatalf("seed error: %v", err) }
	id2, _ := res2.LastInsertId()

	// 2. Setup: Add translations for both
	_, err = GetDB().Exec("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES (?, ?, ?)", id1, "ko", "번역1")
	if err != nil { t.Fatalf("translation seed error: %v", err) }
	_, err = GetDB().Exec("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES (?, ?, ?)", id2, "ko", "번역2")
	if err != nil { t.Fatalf("translation seed error: %v", err) }

	// 3. Action: Merge
	err = MergeTasksWithTitle(ctx, email, []MessageID{MessageID(id1)}, MessageID(id2), "New Merged Title")
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// 4. Verify: Translations should be deleted for both id1 and id2
	var count int
	err = GetDB().QueryRow("SELECT COUNT(*) FROM task_translations WHERE message_id IN (?, ?)", id1, id2).Scan(&count)
	if err != nil { t.Fatalf("verify query error: %v", err) }

	if count != 0 {
		t.Errorf("Expected 0 translations after merge, got %d", count)
	}
}
