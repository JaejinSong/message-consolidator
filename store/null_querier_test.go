package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestNullQuerierHandling(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "test-null@example.com"
	
	// Seed a message
	conn := GetDB()
	res, err := conn.Exec("INSERT INTO messages (user_email, task, source, done, is_deleted, source_ts) VALUES (?, ?, ?, ?, ?, ?)",
		email, "Original Task", "test", 0, 0, "ts-null-1")
	if err != nil {
		t.Fatalf("Failed to seed message: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := int(id64)

	t.Run("MarkMessageDone_NullQuerier", func(t *testing.T) {
		// This should not panic
		err := MarkMessageDone(ctx, nil, email, id, true)
		if err != nil {
			t.Errorf("MarkMessageDone failed: %v", err)
		}

		// Verify change
		var done bool
		err = GetDB().QueryRow("SELECT done FROM messages WHERE id = ?", id).Scan(&done)
		if err != nil || !done {
			t.Errorf("Message was not marked as done: %v", err)
		}
	})

	t.Run("UpdateTaskText_NullQuerier", func(t *testing.T) {
		newTask := "Updated Task via Null Querier"
		err := UpdateTaskText(ctx, nil, email, id, newTask)
		if err != nil {
			t.Errorf("UpdateTaskText failed: %v", err)
		}

		// Verify change
		var task string
		err = GetDB().QueryRow("SELECT task FROM messages WHERE id = ?", id).Scan(&task)
		if err != nil || task != newTask {
			t.Errorf("Expected task %q, got %q (err: %v)", newTask, task, err)
		}
	})

	t.Run("GetMessageByID_NullQuerier", func(t *testing.T) {
		// Invalidate cache to force DB read
		InvalidateCache(email)

		msg, err := GetMessageByID(ctx, nil, email, id)
		if err != nil {
			t.Errorf("GetMessageByID failed: %v", err)
		}
		if msg.ID != id {
			t.Errorf("Expected msg ID %d, got %d", id, msg.ID)
		}
	})

	t.Run("UpdateMessageCategory_NullQuerier", func(t *testing.T) {
		category := "personal"
		err := UpdateMessageCategory(ctx, nil, email, id, category)
		if err != nil {
			t.Errorf("UpdateMessageCategory failed: %v", err)
		}

		// Verify change
		var cat string
		err = GetDB().QueryRow("SELECT category FROM messages WHERE id = ?", id).Scan(&cat)
		if err != nil || cat != category {
			t.Errorf("Expected category %q, got %q (err: %v)", category, cat, err)
		}
	})
}
