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

	conn := GetDB()
	if conn == nil {
		t.Fatal("Expected db to be initialized, got nil")
	}

	//Why: Verifies that all expected core tables are created correctly during the database initialization.
	var name string
	err = GetDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&name)
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
		GetDB().QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "test@example.com").Scan(&count)
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
		GetDB().QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "rollback@example.com").Scan(&count)
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

	email := testutil.RandomEmail("batch")
	ctx := context.Background()

	// Use IDs returned by the DB instead of hardcoded ones to prevent collisions.
	var ids []MessageID
	conn := GetDB()
	for i := 1; i <= 3; i++ {
		ts := testutil.RandomTS(fmt.Sprintf("batch_%d", i))
		res, err := conn.Exec("INSERT INTO messages (user_email, task, source, done, is_deleted, source_ts) VALUES (?, ?, ?, ?, ?, ?)",
			email, fmt.Sprintf("Task %d", i), "slack", 0, 0, ts)
		if err != nil {
			t.Fatalf("Failed to seed message %d: %v", i, err)
		}
		newID, _ := res.LastInsertId()
		ids = append(ids, MessageID(newID))
	}

	t.Run("GetMessagesByIDs", func(t *testing.T) {
		msgs, err := GetMessagesByIDs(ctx, GetDB(), email, ids[:2])
		if err != nil {
			t.Fatalf("Failed to get messages: %v", err)
		}
		if len(msgs) != 2 {
			t.Errorf("Expected 2 messages, got %d", len(msgs))
		}
	})

	//Why: Forces a single database connection for the duration of this specific test to prevent "database is locked" errors in SQLite caused by concurrent background cache refresh operations.
	oldMaxOpen := 20
	conn2 := GetDB()
	conn2.SetMaxOpenConns(1)
	defer conn2.SetMaxOpenConns(oldMaxOpen)

	t.Run("DeleteAndRestoreMessages", func(t *testing.T) {
		//Why: [Step 1/3] Tests soft deletion to ensure records are flagged as deleted without being immediately purged from the database.
		if err := DeleteMessages(context.Background(), GetDB(), email, ids[:2]); err != nil {
			t.Fatalf("Soft delete failed: %v", err)
		}

		var count int
		_ = GetDB().QueryRow("SELECT COUNT(*) FROM messages WHERE is_deleted = 1 AND user_email = ?", email).Scan(&count)
		if count != 2 {
			t.Errorf("Expected 2 soft-deleted messages for this user, got %d", count)
		}

		//Why: [Step 2/3] Tests the restoration of a previously soft-deleted message to ensure users can recover items from the trash.
		if err := RestoreMessages(context.Background(), GetDB(), email, []MessageID{ids[0]}); err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
		_ = GetDB().QueryRow("SELECT COUNT(*) FROM messages WHERE is_deleted = 1 AND user_email = ?", email).Scan(&count)
		if count != 1 {
			t.Errorf("Expected 1 soft-deleted message after restoration, got %d", count)
		}

		//Why: [Step 3/3] Tests hard deletion to verify that records are permanently removed from the database as expected.
		if err := HardDeleteMessages(context.Background(), GetDB(), email, ids); err != nil {
			t.Fatalf("Hard delete failed: %v", err)
		}
		_ = GetDB().QueryRow("SELECT COUNT(*) FROM messages WHERE user_email = ?", email).Scan(&count)
		if count != 0 {
			t.Errorf("Expected 0 messages after hard delete for this user, got %d", count)
		}
	})
}
func TestLegacyColumnRemoval(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	// Why: Verifies that both old column names are now GONE from the schema entirely.
	var count int
	_ = GetDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('contacts') WHERE name='aliases' OR name='legacy_aliases_deprecated'").Scan(&count)
	if count != 0 {
		t.Errorf("Expected 0 columns with legacy names, found %d", count)
	} else {
		t.Logf("Verified hard deprecation: legacy columns dropped successfully.")
	}
}

func TestIdentityXTransitiveLink(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("idx")

	// 1. Create a Master Contact
	masterID, err := AddContact(ctx, tenant, "master-idx@whatap.io", "Master User", "", "test")
	if err != nil {
		t.Fatalf("Failed to create master: %v", err)
	}

	// 2. Create a WhatsApp contact and link to master
	waID := testutil.RandomID("wa")
	waContactID, err := AddContact(ctx, tenant, waID, "WA User", "", ContactTypeWhatsApp)
	if err != nil {
		t.Fatalf("Failed to create WA contact: %v", err)
	}
	if err := LinkContact(ctx, tenant, masterID, waContactID); err != nil {
		t.Fatalf("Failed to link WA contact: %v", err)
	}

	// 3. Resolve WA number — DSU maps waContactID → masterID
	resolvedID, err := ResolveAlias(ctx, "whatsapp", waID)
	if err != nil {
		t.Fatalf("Failed to resolve alias: %v", err)
	}
	if resolvedID != masterID {
		t.Errorf("Expected resolved ID %d, got %d", masterID, resolvedID)
	}

	// 4. Create a Target and Link
	targetID, err := AddContact(ctx, tenant, "target@gmail.com", "Target User", "", "test")
	if err != nil {
		t.Fatalf("Failed to create target: %v", err)
	}
	err = LinkContact(ctx, tenant, masterID, targetID)
	if err != nil {
		t.Fatalf("Failed to link: %v", err)
	}

	// 5. Verify Transitive Resolution
	// Resolving the target ID should now return the master ID due to DSU merge.
	resolvedTargetID := GlobalContactDSU.Find(targetID)
	if resolvedTargetID != masterID {
		t.Errorf("Transitive resolution (DSU) failed: expected %d, got %d", masterID, resolvedTargetID)
	}
}
