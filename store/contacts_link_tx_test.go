package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/internal/testutil"
	"testing"
)

type linkFixture struct {
	tenant               string
	root, master, target int64
	child                int64
}

// Why: master already points at root (one hop) and target owns a child, so linking exercises hop resolution and Flatten.
func seedLinkFixture(t *testing.T) linkFixture {
	t.Helper()
	ctx := context.Background()
	f := linkFixture{tenant: testutil.RandomEmail("link-tx")}
	ids := []*int64{&f.root, &f.master, &f.target, &f.child}
	for i, id := range ids {
		v, err := AddContact(ctx, f.tenant, fmt.Sprintf("c%d-%s", i, f.tenant), fmt.Sprintf("Contact %d", i), "", "test")
		if err != nil {
			t.Fatalf("add contact: %v", err)
		}
		*id = v
	}
	for _, u := range [][2]int64{{f.master, f.root}, {f.child, f.target}} {
		if _, err := GetDB().Exec("UPDATE contacts SET master_contact_id = ? WHERE id = ?", u[1], u[0]); err != nil {
			t.Fatalf("seed master: %v", err)
		}
	}
	GlobalContactDSU.Reset()
	return f
}

func masterOf(t *testing.T, id int64) sql.NullInt64 {
	t.Helper()
	var m sql.NullInt64
	if err := GetDB().QueryRow("SELECT master_contact_id FROM contacts WHERE id = ?", id).Scan(&m); err != nil {
		t.Fatalf("read master: %v", err)
	}
	return m
}

func TestLinkContact_RetriesAfterLockError(t *testing.T) {
	// Arrange
	setupStoreTestDB(t)
	f := seedLinkFixture(t)
	// Why: fires on the Flatten update, after link updates and merge history were written in the failed attempt.
	armLockOnceTrigger(t, fmt.Sprintf(
		`CREATE TRIGGER test_lock_link BEFORE UPDATE ON contacts WHEN OLD.master_contact_id = %d BEGIN SELECT test_lock_once(); END`, f.target))

	// Act
	err := LinkContact(context.Background(), f.tenant, f.master, f.target)

	// Assert
	if err != nil {
		t.Fatalf("LinkContact: %v", err)
	}
	if got := lockOnceCalls.Load(); got != 1 {
		t.Fatalf("injected lock errors = %d, want 1", got)
	}
	if m := masterOf(t, f.target); !m.Valid || m.Int64 != f.root {
		t.Errorf("target master = %v, want effective master %d", m, f.root)
	}
	if m := masterOf(t, f.child); !m.Valid || m.Int64 != f.root {
		t.Errorf("child master = %v, want flattened to %d", m, f.root)
	}
	if m := masterOf(t, f.master); !m.Valid || m.Int64 != f.root {
		t.Errorf("caller master = %v, want unchanged %d", m, f.root)
	}
	var history int
	if err := GetDB().QueryRow("SELECT COUNT(*) FROM identity_merge_history WHERE source_contact_id = ? AND target_contact_id = ?", f.target, f.root).Scan(&history); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if history != 1 {
		t.Errorf("merge history rows = %d, want 1", history)
	}
	if GlobalContactDSU.Find(f.target) != GlobalContactDSU.Find(f.root) {
		t.Errorf("DSU does not join target %d with effective master %d", f.target, f.root)
	}
}

func TestLinkContact_NonLockFailureLeavesDSUUntouched(t *testing.T) {
	// Arrange
	setupStoreTestDB(t)
	f := seedLinkFixture(t)
	if _, err := GetDB().Exec(fmt.Sprintf(
		`CREATE TRIGGER test_fail_link BEFORE UPDATE ON contacts WHEN OLD.master_contact_id = %d BEGIN SELECT RAISE(ABORT, 'boom'); END`, f.target)); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	// Act
	err := LinkContact(context.Background(), f.tenant, f.master, f.target)

	// Assert
	if err == nil {
		t.Fatal("expected error from failing flatten")
	}
	if GlobalContactDSU.Find(f.target) == GlobalContactDSU.Find(f.root) {
		t.Errorf("DSU unioned target %d with %d despite rolled-back tx", f.target, f.root)
	}
	if m := masterOf(t, f.target); m.Valid {
		t.Errorf("target master = %v, want rolled back to NULL", m)
	}
}
