package store

import (
	"context"
	"database/sql"
	"errors"
	"message-consolidator/internal/testutil"
	"testing"
)

// deleteDisplayNameResolution removes the contact_resolution row for a display name so that
// EnsureContactAlias can run without the pre-existing resolution short-circuiting it.
// This simulates contacts that were saved without display-name resolution (e.g. via raw SQL import).
func deleteDisplayNameResolution(t *testing.T, ctx context.Context, tenant, displayName string) {
	t.Helper()
	norm := NormalizeIdentifier(displayName)
	_, err := GetDB().ExecContext(ctx,
		"DELETE FROM contact_resolution WHERE tenant_email=? AND raw_identifier=?",
		tenant, norm)
	if err != nil {
		t.Fatalf("deleteDisplayNameResolution: %v", err)
	}
}

func TestEnsureContactAlias(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()

	t.Run("happy path: single master -> alias created", func(t *testing.T) {
		tenant := testutil.RandomEmail("tenant")
		_, err := UpsertContact(ctx, tenant, "alice@example.com", "Alice Smith", "", "test")
		if err != nil {
			t.Fatalf("UpsertContact: %v", err)
		}
		// Why: UpsertContact auto-registers the display-name resolution; remove it so
		// EnsureContactAlias does not exit early at the GetResolutionsByIdentifiers check.
		deleteDisplayNameResolution(t, ctx, tenant, "Alice Smith")

		if err := EnsureContactAlias(ctx, tenant, "Alice Smith"); err != nil {
			t.Fatalf("EnsureContactAlias: %v", err)
		}

		var aliasID int64
		var masterContactID sql.NullInt64
		var source string
		err = GetDB().QueryRowContext(ctx,
			"SELECT id, master_contact_id, source FROM contacts WHERE tenant_email=? AND canonical_id=?",
			tenant, "Alice Smith").Scan(&aliasID, &masterContactID, &source)
		if err != nil {
			t.Fatalf("alias row not found: %v", err)
		}
		if !masterContactID.Valid {
			t.Error("expected master_contact_id to be set")
		}
		if source != "auto" {
			t.Errorf("source = %q, want auto", source)
		}
	})

	t.Run("zero master match -> no-op", func(t *testing.T) {
		tenant := testutil.RandomEmail("tenant")
		if err := EnsureContactAlias(ctx, tenant, "Unknown Person"); err != nil {
			t.Fatalf("EnsureContactAlias: %v", err)
		}

		var dummy int64
		err := GetDB().QueryRowContext(ctx,
			"SELECT id FROM contacts WHERE tenant_email=? AND canonical_id=?",
			tenant, "Unknown Person").Scan(&dummy)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected no row for unknown name, got err=%v", err)
		}
	})

	t.Run("ambiguous: two masters same display_name -> no-op", func(t *testing.T) {
		tenant := testutil.RandomEmail("tenant")
		_, err := UpsertContact(ctx, tenant, "alice1@example.com", "Alice Smith", "", "test")
		if err != nil {
			t.Fatalf("UpsertContact alice1: %v", err)
		}
		_, err = UpsertContact(ctx, tenant, "alice2@example.com", "Alice Smith", "", "test")
		if err != nil {
			t.Fatalf("UpsertContact alice2: %v", err)
		}
		deleteDisplayNameResolution(t, ctx, tenant, "Alice Smith")

		if err := EnsureContactAlias(ctx, tenant, "Alice Smith"); err != nil {
			t.Fatalf("EnsureContactAlias: %v", err)
		}

		// No alias row (source="auto") should exist for canonical_id="Alice Smith".
		var source string
		err = GetDB().QueryRowContext(ctx,
			"SELECT source FROM contacts WHERE tenant_email=? AND canonical_id=? AND source=?",
			tenant, "Alice Smith", "auto").Scan(&source)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected no alias row with source=auto, got source=%q err=%v", source, err)
		}
	})

	t.Run("idempotent: second call is no-op", func(t *testing.T) {
		tenant := testutil.RandomEmail("tenant")
		_, err := UpsertContact(ctx, tenant, "bob@example.com", "Bob Jones", "", "test")
		if err != nil {
			t.Fatalf("UpsertContact bob: %v", err)
		}
		deleteDisplayNameResolution(t, ctx, tenant, "Bob Jones")

		if err := EnsureContactAlias(ctx, tenant, "Bob Jones"); err != nil {
			t.Fatalf("first EnsureContactAlias: %v", err)
		}
		if err := EnsureContactAlias(ctx, tenant, "Bob Jones"); err != nil {
			t.Fatalf("second EnsureContactAlias: %v", err)
		}

		rows, err := GetDB().QueryContext(ctx,
			"SELECT id FROM contacts WHERE tenant_email=? AND canonical_id=? AND source=?",
			tenant, "Bob Jones", "auto")
		if err != nil {
			t.Fatalf("query alias rows: %v", err)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows iteration: %v", err)
		}
		if count != 1 {
			t.Errorf("expected exactly 1 alias row, got %d", count)
		}
	})

	t.Run("raw value contains @ -> early exit", func(t *testing.T) {
		tenant := testutil.RandomEmail("tenant")
		_, err := UpsertContact(ctx, tenant, "carol@example.com", "carol@example.com", "", "test")
		if err != nil {
			t.Fatalf("UpsertContact carol: %v", err)
		}

		if err := EnsureContactAlias(ctx, tenant, "carol@example.com"); err != nil {
			t.Fatalf("EnsureContactAlias: %v", err)
		}

		// No alias row should be created since rawValue contains @; function exits early.
		var source string
		err = GetDB().QueryRowContext(ctx,
			"SELECT source FROM contacts WHERE tenant_email=? AND canonical_id=? AND source=?",
			tenant, "carol@example.com", "auto").Scan(&source)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected no alias with source=auto for email-shaped value, got source=%q err=%v", source, err)
		}
	})

	t.Run("empty raw value -> no-op", func(t *testing.T) {
		tenant := testutil.RandomEmail("tenant")
		if err := EnsureContactAlias(ctx, tenant, ""); err != nil {
			t.Fatalf("EnsureContactAlias with empty string: %v", err)
		}
	})
}
