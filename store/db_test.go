package store

import (
	"message-consolidator/internal/testutil"
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestInitDBLocal(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	if db == nil {
		t.Fatal("Expected db to be initialized, got nil")
	}

	//Why: Verifies that all expected core tables are created correctly during the database initialization.
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&name)
	if err != nil {
		t.Errorf("Failed to find users table: %v", err)
	}
	if name != "users" {
		t.Errorf("Expected table 'users', got '%s'", name)
	}
}

func TestRunInTx(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()

	t.Run("Commit", func(t *testing.T) {
		err := RunInTx(ctx, func(tx *sql.Tx) error {
			_, err := tx.Exec("INSERT INTO users (email, name) VALUES (?, ?)", "test@example.com", "Test User")
			return err
		})
		if err != nil {
			t.Errorf("Transaction failed: %v", err)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "test@example.com").Scan(&count)
		if count != 1 {
			t.Errorf("Expected 1 user, got %d", count)
		}
	})

	t.Run("Rollback", func(t *testing.T) {
		err := RunInTx(ctx, func(tx *sql.Tx) error {
			_, _ = tx.Exec("INSERT INTO users (email, name) VALUES (?, ?)", "rollback@example.com", "Rollback User")
			return fmt.Errorf("force rollback")
		})
		if err == nil {
			t.Error("Expected error for rollback, got nil")
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "rollback@example.com").Scan(&count)
		if count != 0 {
			t.Errorf("Expected 0 users after rollback, got %d", count)
		}
	})
}

func TestBatchOperations(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	ctx := context.Background()

	//Why: Seeds initial test data to provide a baseline for operation verification.
	for i := 1; i <= 3; i++ {
		_, err := db.Exec("INSERT INTO messages (id, user_email, task, source, done, is_deleted, source_ts) VALUES (?, ?, ?, ?, ?, ?, ?)",
			i, email, fmt.Sprintf("Task %d", i), "slack", 0, 0, fmt.Sprintf("ts_%d", i))
		if err != nil {
			t.Fatalf("Failed to seed message %d: %v", i, err)
		}
	}

	t.Run("GetMessagesByIDs", func(t *testing.T) {
		msgs, err := GetMessagesByIDs(ctx, email, []int{1, 2})
		if err != nil {
			t.Fatalf("Failed to get messages: %v", err)
		}
		if len(msgs) != 2 {
			t.Errorf("Expected 2 messages, got %d", len(msgs))
		}
	})

	//Why: Forces a single database connection for the duration of this specific test to prevent "database is locked" errors in SQLite caused by concurrent background cache refresh operations.
	oldMaxOpen := 20
	db.SetMaxOpenConns(1)
	defer db.SetMaxOpenConns(oldMaxOpen)

	t.Run("DeleteAndRestoreMessages", func(t *testing.T) {
		//Why: [Step 1/3] Tests soft deletion to ensure records are flagged as deleted without being immediately purged from the database.
		if err := DeleteMessages(email, []int{1, 2}); err != nil {
			t.Fatalf("Soft delete failed: %v", err)
		}

		var count int
		_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE is_deleted = 1").Scan(&count)
		if count != 2 {
			t.Errorf("Expected 2 soft-deleted messages, got %d", count)
		}

		//Why: [Step 2/3] Tests the restoration of a previously soft-deleted message to ensure users can recover items from the trash.
		if err := RestoreMessages(email, []int{1}); err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
		_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE is_deleted = 1").Scan(&count)
		if count != 1 {
			t.Errorf("Expected 1 soft-deleted message after restoration, got %d", count)
		}

		//Why: [Step 3/3] Tests hard deletion to verify that records are permanently removed from the database as expected.
		if err := HardDeleteMessages(email, []int{1, 2, 3}); err != nil {
			t.Fatalf("Hard delete failed: %v", err)
		}
		_ = db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
		if count != 0 {
			t.Errorf("Expected 0 messages after hard delete, got %d", count)
		}
	})
}
