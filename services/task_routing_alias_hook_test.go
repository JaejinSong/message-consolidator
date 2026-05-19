package services

import (
	"context"
	"database/sql"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

// Why: Pins the contract that createTaskFromItem calls EnsureContactAlias for the requester,
// so a display-name requester auto-links to the master contact and v_messages.requester_canonical
// resolves to the master's canonical_id. Regression guard for the alias-hook wiring.
func TestCreateTaskFromItem_EnsuresContactAlias(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "integration-alias-test@example.com"

	// Why: Insert master contact via raw SQL to skip upsertResolutionForContact. UpsertContact
	// would register "test requester" in contact_resolution, causing EnsureContactAlias to
	// early-return (already resolved). Raw insert leaves only the canonical_id in the resolution
	// table, so the display_name path through EnsureContactAlias is exercised.
	res, err := store.GetDB().ExecContext(ctx,
		"INSERT INTO contacts (tenant_email, canonical_id, display_name, source) VALUES (?, 'master@example.com', 'Test Requester', 'manual')",
		email,
	)
	if err != nil {
		t.Fatalf("insert master contact: %v", err)
	}
	masterRowID, _ := res.LastInsertId()
	// Register only the canonical_id ("master@example.com") in the resolution table so
	// GetResolutionsByIdentifiers("test requester") returns nothing, unblocking alias creation.
	_, err = store.GetDB().ExecContext(ctx,
		"INSERT INTO contact_resolution (tenant_email, raw_identifier, contact_id) VALUES (?, 'master@example.com', ?)",
		email, masterRowID,
	)
	if err != nil {
		t.Fatalf("seed contact_resolution: %v", err)
	}

	// Create user so SaveMessage has a valid user_email foreign key.
	if _, err := store.GetOrCreateUser(ctx, email, "Owner", ""); err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}

	item := store.TodoItem{
		State:     "new",
		Task:      "Test Task",
		Requester: "Test Requester",
		Assignee:  "Someone Else",
	}
	msg := store.ConsolidatedMessage{
		UserEmail: email,
		Source:    "slack",
		Room:      "test-room-alias",
	}

	if _, err := HandleTaskState(ctx, nil, email, item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}

	// Assert alias row exists in contacts with master_contact_id pointing at the master.
	var masterContactID sql.NullInt64
	var source string
	err = store.GetDB().QueryRowContext(ctx,
		"SELECT master_contact_id, source FROM contacts WHERE tenant_email=? AND canonical_id=?",
		email, "Test Requester",
	).Scan(&masterContactID, &source)
	if err != nil {
		t.Fatalf("alias not found: %v", err)
	}
	if !masterContactID.Valid {
		t.Error("expected master_contact_id to be set on alias")
	}
	if source != "auto" {
		t.Errorf("source = %q, want auto", source)
	}

	// Assert v_messages.requester_canonical resolves to master's canonical_id via the alias link.
	var requesterCanonical string
	err = store.GetDB().QueryRowContext(ctx,
		"SELECT COALESCE(requester_canonical, '') FROM v_messages WHERE user_email=? AND room=? ORDER BY id DESC LIMIT 1",
		email, "test-room-alias",
	).Scan(&requesterCanonical)
	if err != nil {
		t.Fatalf("v_messages query: %v", err)
	}
	if requesterCanonical != "master@example.com" {
		t.Errorf("requester_canonical = %q, want master@example.com", requesterCanonical)
	}
}
