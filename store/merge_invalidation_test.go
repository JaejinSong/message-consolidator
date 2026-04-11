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
	res1, err := db.Exec("INSERT INTO messages (user_email, task, source, done, is_deleted, source_ts) VALUES (?, ?, ?, ?, ?, ?)",
		email, "Source Task", "slack", 0, 0, "ts-merge-1")
	if err != nil { t.Fatalf("seed error: %v", err) }
	id1, _ := res1.LastInsertId()

	res2, err := db.Exec("INSERT INTO messages (user_email, task, source, done, is_deleted, source_ts) VALUES (?, ?, ?, ?, ?, ?)",
		email, "Dest Task", "slack", 0, 0, "ts-merge-2")
	if err != nil { t.Fatalf("seed error: %v", err) }
	id2, _ := res2.LastInsertId()

	// 2. Setup: Add translations for both
	_, err = db.Exec("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES (?, ?, ?)", id1, "ko", "번역1")
	if err != nil { t.Fatalf("translation seed error: %v", err) }
	_, err = db.Exec("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES (?, ?, ?)", id2, "ko", "번역2")
	if err != nil { t.Fatalf("translation seed error: %v", err) }

	// 3. Action: Merge
	err = MergeTasksWithTitle(ctx, email, []int64{id1}, id2, "New Merged Title")
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// 4. Verify: Translations should be deleted for both id1 and id2
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM task_translations WHERE message_id IN (?, ?)", id1, id2).Scan(&count)
	if err != nil { t.Fatalf("verify query error: %v", err) }

	if count != 0 {
		t.Errorf("Expected 0 translations after merge, got %d", count)
	}
}
