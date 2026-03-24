package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestInitDBLocal(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	if db == nil {
		t.Fatal("Expected db to be initialized, got nil")
	}

	// Verify tables exist
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
	cleanup, err := SetupTestDB()
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
