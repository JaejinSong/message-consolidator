package services

import (
	"context"
	"database/sql/driver"
	"errors"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"strings"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

var (
	lockOnceArmed atomic.Bool
	lockOnceCalls atomic.Int32
)

func init() {
	// Why: lets a test make the first v_messages read fail with a lock error without any production seam.
	sqlite.MustRegisterScalarFunction("test_lock_once", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		if lockOnceArmed.Swap(false) {
			lockOnceCalls.Add(1)
			return nil, errors.New("database is locked (5) (SQLITE_BUSY)")
		}
		return int64(0), nil
	})
}

// Why: wraps v_messages so the next read errors once; the original view is kept as v_messages_orig.
func armFirstReadLockError(t *testing.T) {
	t.Helper()
	var viewSQL string
	if err := store.GetDB().QueryRow("SELECT sql FROM sqlite_master WHERE type='view' AND name='v_messages'").Scan(&viewSQL); err != nil {
		t.Fatalf("read view sql: %v", err)
	}
	origSQL := strings.Replace(viewSQL, "v_messages", "v_messages_orig", 1)
	stmts := []string{
		"DROP VIEW v_messages",
		origSQL,
		"CREATE VIEW v_messages AS SELECT * FROM v_messages_orig WHERE test_lock_once() = 0",
	}
	for _, s := range stmts {
		if _, err := store.GetDB().Exec(s); err != nil {
			t.Fatalf("wrap view (%.40s): %v", s, err)
		}
	}
	lockOnceCalls.Store(0)
	lockOnceArmed.Store(true)
	t.Cleanup(func() { lockOnceArmed.Store(false) })
}

// Why: RunInTx re-runs the routing closure after a lock error; a stale dropped=true from the
// failed attempt used to make handleResolve report "dropped" although the retry committed.
func TestHandleResolveCancel_RetryAfterLockError(t *testing.T) {
	tests := []struct {
		name         string
		state        string
		wantReturnID bool
		check        string
	}{
		{"resolve returns id and marks done", "resolve", true, "SELECT done FROM messages WHERE id = ?"},
		{"cancel soft-deletes", "cancel", false, "SELECT is_deleted FROM messages WHERE id = ?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			defer cleanup()
			email := "retry-test@example.com"
			res, err := store.GetDB().Exec(
				"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted) VALUES (?, 'slack', 'Retry-Room', 'T', 'o', 'ts-retry', 0, 0)", email)
			if err != nil {
				t.Fatalf("seed: %v", err)
			}
			id64, _ := res.LastInsertId()
			id := store.MessageID(id64)
			armFirstReadLockError(t)
			item := store.TodoItem{ID: &id, State: tt.state}
			msg := store.ConsolidatedMessage{UserEmail: email, Source: "slack", Room: "Retry-Room", OriginalText: "done now"}

			// Act
			got, err := HandleTaskState(context.Background(), nil, email, item, msg)

			// Assert
			if err != nil {
				t.Fatalf("HandleTaskState: %v", err)
			}
			if lockOnceCalls.Load() != 1 {
				t.Fatalf("injected lock errors = %d, want 1", lockOnceCalls.Load())
			}
			if tt.wantReturnID && got != id {
				t.Errorf("returned id = %d, want %d", got, id)
			}
			var flag int
			if err := store.GetDB().QueryRow(tt.check, id).Scan(&flag); err != nil {
				t.Fatalf("read: %v", err)
			}
			if flag != 1 {
				t.Errorf("flag = %d, want 1", flag)
			}
		})
	}
}

// Why: seeds an open task parked by exclusion (excluded_at set) so handleUpdate must auto-restore it.
func seedExcludedTask(t *testing.T, email string) store.MessageID {
	t.Helper()
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted, excluded_at) VALUES (?, 'slack', 'Retry-Room', 'Old', 'o', 'ts-excl', 0, 0, '2026-01-01T00:00:00Z')", email)
	if err != nil {
		t.Fatalf("seed excluded: %v", err)
	}
	id64, _ := res.LastInsertId()
	return store.MessageID(id64)
}

func TestHandleUpdate_ExcludedTaskRestore(t *testing.T) {
	tests := []struct {
		name         string
		trigger      string
		wantLockHit  int32
		wantRestored bool
	}{
		{
			"lock error on restore re-runs the tx and restores once",
			`CREATE TRIGGER test_restore_lock BEFORE UPDATE OF excluded_at ON messages BEGIN SELECT test_lock_once(); END`,
			1, true,
		},
		{
			"non-lock restore error is swallowed and the content update still applies",
			`CREATE TRIGGER test_restore_fail BEFORE UPDATE OF excluded_at ON messages BEGIN SELECT RAISE(ABORT, 'boom'); END`,
			0, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			defer cleanup()
			email := "restore-test@example.com"
			id := seedExcludedTask(t, email)
			if _, err := store.GetDB().Exec(tt.trigger); err != nil {
				t.Fatalf("create trigger: %v", err)
			}
			lockOnceCalls.Store(0)
			lockOnceArmed.Store(tt.wantLockHit > 0)
			t.Cleanup(func() { lockOnceArmed.Store(false) })
			item := store.TodoItem{ID: &id, State: "update", Task: "New title"}
			msg := store.ConsolidatedMessage{UserEmail: email, Source: "slack", Room: "Retry-Room", OriginalText: "new activity"}

			// Act
			got, err := HandleTaskState(context.Background(), nil, email, item, msg)

			// Assert
			if err != nil {
				t.Fatalf("HandleTaskState: %v", err)
			}
			if got != id {
				t.Errorf("returned id = %d, want %d (dropped flag must be reset per attempt)", got, id)
			}
			if lockOnceCalls.Load() != tt.wantLockHit {
				t.Errorf("injected lock errors = %d, want %d", lockOnceCalls.Load(), tt.wantLockHit)
			}
			var excluded *string
			var task, orig string
			if err := store.GetDB().QueryRow("SELECT excluded_at, task, original_text FROM messages WHERE id = ?", id).Scan(&excluded, &task, &orig); err != nil {
				t.Fatalf("read: %v", err)
			}
			if (excluded == nil) != tt.wantRestored {
				t.Errorf("excluded_at restored = %v, want %v", excluded == nil, tt.wantRestored)
			}
			if task != "New title" {
				t.Errorf("task = %q, want New title", task)
			}
			if n := strings.Count(orig, "new activity"); n != 1 {
				t.Errorf("original_text has %d appends of the update, want exactly 1: %q", n, orig)
			}
		})
	}
}
