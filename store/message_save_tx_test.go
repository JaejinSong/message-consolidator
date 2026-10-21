package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"message-consolidator/internal/testutil"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

var (
	lockOnceArmed atomic.Bool
	lockOnceCalls atomic.Int32
)

func init() {
	// Why: lets a test make one statement fail with a lock error via a trigger, without any production seam.
	sqlite.MustRegisterScalarFunction("test_lock_once", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		if lockOnceArmed.Swap(false) {
			lockOnceCalls.Add(1)
			return nil, errors.New("database is locked (5) (SQLITE_BUSY)")
		}
		return int64(0), nil
	})
}

// Why: installs a trigger whose body calls test_lock_once and arms it; the first firing errors, later ones pass.
func armLockOnceTrigger(t *testing.T, triggerSQL string) {
	t.Helper()
	if _, err := GetDB().Exec(triggerSQL); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	lockOnceCalls.Store(0)
	lockOnceArmed.Store(true)
	t.Cleanup(func() { lockOnceArmed.Store(false) })
}

func setupStoreTestDB(t *testing.T) {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(cleanup)
}

func TestSaveMessage_LockErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		useTx     bool
		wantSaved bool
		wantErr   bool
		wantRows  int
	}{
		{"inside tx the lock error propagates without statement retry", true, false, true, 0},
		{"outside tx a transient lock error is retried and succeeds", false, true, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			setupStoreTestDB(t)
			ctx := context.Background()
			email := testutil.RandomEmail("save-lock")
			armLockOnceTrigger(t, `CREATE TRIGGER test_lock_insert BEFORE INSERT ON messages BEGIN SELECT test_lock_once(); END`)
			msg := ConsolidatedMessage{UserEmail: email, Source: "slack", Room: "Lock-Room", Task: "unique lock task", OriginalText: "o", SourceTS: testutil.RandomTS("lock")}
			var q Querier = GetDB()
			if tt.useTx {
				tx, err := GetDB().BeginTx(ctx, nil)
				if err != nil {
					t.Fatalf("begin: %v", err)
				}
				defer func() { _ = tx.Rollback() }()
				q = tx
			}

			// Act
			saved, _, err := SaveMessage(ctx, q, msg)

			// Assert
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !isLockError(err) {
				t.Errorf("err = %v, want lock error", err)
			}
			if saved != tt.wantSaved {
				t.Errorf("saved = %v, want %v", saved, tt.wantSaved)
			}
			if got := lockOnceCalls.Load(); got != 1 {
				t.Errorf("CreateMessage attempts that hit the lock = %d, want 1", got)
			}
			if tx, ok := q.(*sql.Tx); ok {
				_ = tx.Rollback()
			}
			var rows int
			if err := GetDB().QueryRow("SELECT COUNT(*) FROM messages WHERE user_email = ?", email).Scan(&rows); err != nil {
				t.Fatalf("count: %v", err)
			}
			if rows != tt.wantRows {
				t.Errorf("rows = %d, want %d", rows, tt.wantRows)
			}
		})
	}
}
