package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
)

// TestAddConfirmedAtColumn_BackfillsOnceOnly pins the trap in this migration. The
// backfill has to run exactly on the call that adds the column: new tasks are meant to
// arrive with confirmed_at NULL, so an unconditional "UPDATE ... WHERE confirmed_at IS
// NULL" would silently confirm every fresh row the next time the process started, and the
// inbox signal would read as empty forever.
func TestAddConfirmedAtColumn_BackfillsOnceOnly(t *testing.T) {
	raw, err := sql.Open("sqlite", fmt.Sprintf("file:confirmedat_%d?mode=memory", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	if _, err := raw.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_email TEXT, task TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO messages (user_email, task, created_at)
		VALUES ('u@test.com','pre-existing one', '2026-01-01 00:00:00'),
		       ('u@test.com','pre-existing two', '2026-02-02 00:00:00')`); err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}

	ctx := context.Background()
	if err := addConfirmedAtColumn(ctx, raw); err != nil {
		t.Fatalf("first run: %v", err)
	}

	var unconfirmed int
	count := func() int {
		if err := raw.QueryRow(`SELECT COUNT(*) FROM messages WHERE confirmed_at IS NULL`).Scan(&unconfirmed); err != nil {
			t.Fatalf("count: %v", err)
		}
		return unconfirmed
	}
	if n := count(); n != 0 {
		t.Errorf("%d pre-existing rows left unconfirmed; the inbox must start empty", n)
	}

	// A task extracted after the migration arrives unconfirmed: it is an inbox item.
	if _, err := raw.Exec(`INSERT INTO messages (user_email, task) VALUES ('u@test.com','freshly extracted')`); err != nil {
		t.Fatalf("insert new row: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("new row unconfirmed count = %d, want 1", n)
	}

	// Re-running must leave the inbox item alone.
	if err := addConfirmedAtColumn(ctx, raw); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := count(); n != 1 {
		t.Errorf("re-run confirmed the inbox item (unconfirmed = %d); the backfill must be one-time", n)
	}
}

// TestEngagementConfirms covers the inference that replaces a confirm button: marking a
// task done, or editing it, is engagement and therefore confirmation. Deleting is not.
func TestEngagementConfirms(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	const email = "confirm@test.com"
	conn := GetDB()

	newTask := func(task string) MessageID {
		res, err := conn.ExecContext(ctx,
			`INSERT INTO messages (user_email, source, room, task, assignee, done, is_deleted)
			 VALUES (?, 'slack', 'r', ?, 'shared', 0, 0)`, email, task)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		id, _ := res.LastInsertId()
		return MessageID(id)
	}
	confirmedAt := func(id MessageID) sql.NullString {
		var v sql.NullString
		if err := conn.QueryRowContext(ctx,
			`SELECT confirmed_at FROM messages WHERE id = ?`, int64(id)).Scan(&v); err != nil {
			t.Fatalf("read confirmed_at: %v", err)
		}
		return v
	}

	untouched := newTask("never engaged")
	if confirmedAt(untouched).Valid {
		t.Error("a freshly extracted task must start unconfirmed")
	}

	doneID := newTask("will be completed")
	if err := markMessageDoneTrue(ctx, nil, email, doneID); err != nil {
		t.Fatalf("markMessageDoneTrue: %v", err)
	}
	if !confirmedAt(doneID).Valid {
		t.Error("marking a task done must confirm it")
	}
}
