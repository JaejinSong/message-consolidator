package store

import (
	"message-consolidator/internal/testutil"
	"context"
	"testing"
)

// TestMetadataIntegrity verifies the mapping of SQLite-specific types to Go struct fields.
// Why: Ensures that 'is_context_query' (Integer 0/1) and 'constraints' (JSON String) are correctly handled during scanning.
func TestMetadataIntegrity(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	userEmail := "test@example.com"

	// Why: Seeds raw metadata including a context-query flag (1) and a JSON-encoded array of behavioral constraints.
	_, err = db.Exec(`INSERT INTO messages 
		(id, user_email, source, task, is_context_query, constraints, source_ts) 
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		101, userEmail, "slack", "Test Policy Task", 1, `["Must use Inter font", "No px allowed"]`, "ts_101")
	if err != nil {
		t.Fatalf("Failed to seed metadata: %v", err)
	}

	t.Run("ScanIsContextQuery", func(t *testing.T) {
		msg, err := GetMessageByID(ctx, 101)
		if err != nil {
			t.Fatalf("GetMessageByID failed: %v", err)
		}

		// Why: Explicitly verifies that INTEGER 1 from SQLite is correctly interpreted as 'true' in Go.
		if !msg.IsContextQuery {
			t.Errorf("Expected IsContextQuery to be true, got false")
		}
	})

	t.Run("ScanConstraintsJSON", func(t *testing.T) {
		msg, err := GetMessageByID(ctx, 101)
		if err != nil {
			t.Fatalf("GetMessageByID failed: %v", err)
		}

		if len(msg.Constraints) != 2 {
			t.Fatalf("Expected 2 constraints, got %d", len(msg.Constraints))
		}

		// Why: Verifies the integrity of JSON unmarshaling into the string slice.
		expected := "Must use Inter font"
		if msg.Constraints[0] != expected {
			t.Errorf("Expected constraint[0] to be '%s', got '%s'", expected, msg.Constraints[0])
		}
	})

	t.Run("ScanDefaultEmptyConstraints", func(t *testing.T) {
		_, _ = db.Exec("INSERT INTO messages (id, user_email, source, task, source_ts) VALUES (?, ?, ?, ?, ?)", 
			102, userEmail, "slack", "No metadata", "ts_102")
		
		msg, err := GetMessageByID(ctx, 102)
		if err != nil {
			t.Fatalf("GetMessageByID failed: %v", err)
		}

		// Why: Ensures that missing or null constraints result in a non-nil empty slice to prevent frontend crashes.
		if msg.Constraints == nil {
			t.Fatal("Expected Constraints to be empty slice, got nil")
		}
		if len(msg.Constraints) != 0 {
			t.Errorf("Expected 0 constraints, got %d", len(msg.Constraints))
		}
	})
}
