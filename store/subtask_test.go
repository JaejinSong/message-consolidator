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
		err := UpdateSubtaskStatus(ctx, db, email, MessageID(id), 0, true)
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
		err := UpdateSubtaskStatus(ctx, db, email, MessageID(id), 0, false)
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
		err := UpdateSubtaskStatus(ctx, db, email, MessageID(id), 99, true)
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

func TestUpdateSubtaskStatus_ReversePropagation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "test@example.com"
	db := GetDB()

	t.Run("all subtasks done - parent auto-closes", func(t *testing.T) {
		subtasks := []Subtask{
			{Task: "Step 1", Done: false},
			{Task: "Step 2", Done: false},
		}
		subtasksJSON, _ := json.Marshal(subtasks)
		res, err := db.Exec(`INSERT INTO messages (user_email, task, source, subtasks, done) VALUES (?, ?, ?, ?, ?)`,
			email, "Parent Task", "gmail", string(subtasksJSON), 0)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		id, _ := res.LastInsertId()

		// Mark first subtask done — parent should stay open
		if err := UpdateSubtaskStatus(ctx, db, email, MessageID(id), 0, true); err != nil {
			t.Fatalf("toggle subtask 0: %v", err)
		}
		var done int
		_ = db.QueryRow("SELECT done FROM messages WHERE id=?", id).Scan(&done)
		if done != 0 {
			t.Error("parent should still be open after only one subtask done")
		}

		// Mark second subtask done — should trigger auto-close
		if err := UpdateSubtaskStatus(ctx, db, email, MessageID(id), 1, true); err != nil {
			t.Fatalf("toggle subtask 1: %v", err)
		}
		_ = db.QueryRow("SELECT done FROM messages WHERE id=?", id).Scan(&done)
		if done != 1 {
			t.Error("expected parent to be auto-closed when all subtasks are done")
		}
	})

	t.Run("partial subtasks done - parent stays open", func(t *testing.T) {
		subtasks := []Subtask{
			{Task: "Step A", Done: false},
			{Task: "Step B", Done: false},
		}
		subtasksJSON, _ := json.Marshal(subtasks)
		res, err := db.Exec(`INSERT INTO messages (user_email, task, source, subtasks, done) VALUES (?, ?, ?, ?, ?)`,
			email, "Partial Parent", "gmail", string(subtasksJSON), 0)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		id, _ := res.LastInsertId()

		if err := UpdateSubtaskStatus(ctx, db, email, MessageID(id), 0, true); err != nil {
			t.Fatalf("toggle subtask 0: %v", err)
		}
		var done int
		_ = db.QueryRow("SELECT done FROM messages WHERE id=?", id).Scan(&done)
		if done != 0 {
			t.Error("parent should remain open when only partial subtasks are done")
		}
	})

	t.Run("toggle subtask off - no auto-close", func(t *testing.T) {
		subtasks := []Subtask{
			{Task: "Step X", Done: true},
			{Task: "Step Y", Done: false},
		}
		subtasksJSON, _ := json.Marshal(subtasks)
		res, err := db.Exec(`INSERT INTO messages (user_email, task, source, subtasks, done) VALUES (?, ?, ?, ?, ?)`,
			email, "Toggle-off Parent", "gmail", string(subtasksJSON), 0)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		id, _ := res.LastInsertId()

		// Toggling subtask 1 OFF (done=false) must not panic or auto-close
		if err := UpdateSubtaskStatus(ctx, db, email, MessageID(id), 1, false); err != nil {
			t.Fatalf("toggle subtask 1 off: %v", err)
		}
		var done int
		_ = db.QueryRow("SELECT done FROM messages WHERE id=?", id).Scan(&done)
		if done != 0 {
			t.Error("parent should stay open when toggling subtask off")
		}
	})
}
