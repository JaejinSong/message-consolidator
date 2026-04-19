package store

import (
	"context"
	"encoding/json"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestUpdateSubtaskStatus(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "test@example.com"
	db := GetDB()

	// 1. Seed a message with subtasks
	subtasks := []Subtask{
		{Task: "Action 1", Done: false},
		{Task: "Action 2", Done: false},
	}
	subtasksJSON, _ := json.Marshal(subtasks)

	res, err := db.Exec(`
		INSERT INTO messages (user_email, task, source, subtasks, done) 
		VALUES (?, ?, ?, ?, ?)`,
		email, "Main Task", "gmail", string(subtasksJSON), 0)
	if err != nil {
		t.Fatalf("Failed to seed message: %v", err)
	}
	id, _ := res.LastInsertId()

	t.Run("ToggleSubtaskDone", func(t *testing.T) {
		// Toggle first subtask to DONE
		err := UpdateSubtaskStatus(ctx, db, email, int(id), 0, true)
		if err != nil {
			t.Fatalf("UpdateSubtaskStatus failed: %v", err)
		}

		// Verify
		var updatedJSON string
		err = db.QueryRow("SELECT subtasks FROM messages WHERE id = ?", id).Scan(&updatedJSON)
		if err != nil {
			t.Fatalf("Failed to fetch updated message: %v", err)
		}

		var updatedSubtasks []Subtask
		json.Unmarshal([]byte(updatedJSON), &updatedSubtasks)

		if len(updatedSubtasks) != 2 {
			t.Errorf("Expected 2 subtasks, got %d", len(updatedSubtasks))
		}
		if !updatedSubtasks[0].Done {
			t.Error("Subtask 0 should be done")
		}
		if updatedSubtasks[1].Done {
			t.Error("Subtask 1 should still be not done")
		}
	})

	t.Run("ToggleSubtaskBackToNotDone", func(t *testing.T) {
		// Toggle first subtask back to NOT DONE
		err := UpdateSubtaskStatus(ctx, db, email, int(id), 0, false)
		if err != nil {
			t.Fatalf("UpdateSubtaskStatus failed: %v", err)
		}

		// Verify
		var updatedJSON string
		db.QueryRow("SELECT subtasks FROM messages WHERE id = ?", id).Scan(&updatedJSON)
		var updatedSubtasks []Subtask
		json.Unmarshal([]byte(updatedJSON), &updatedSubtasks)

		if updatedSubtasks[0].Done {
			t.Error("Subtask 0 should be not done after toggle back")
		}
	})

	t.Run("InvalidIndex", func(t *testing.T) {
		err := UpdateSubtaskStatus(ctx, db, email, int(id), 99, true)
		if err == nil {
			t.Error("Expected error for invalid subtask index, got nil")
		}
	})

	t.Run("NonExistentTask", func(t *testing.T) {
		err := UpdateSubtaskStatus(ctx, db, email, 9999, 0, true)
		if err == nil {
			t.Error("Expected error for non-existent task ID, got nil")
		}
	})
}
