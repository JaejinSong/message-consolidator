package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

var errTestLocked = errors.New("database is locked (5) (SQLITE_BUSY)")

func useTempWALConn(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tx_retry.db")
	c, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatalf("open temp db: %v", err)
	}
	prev := conn
	conn = c
	t.Cleanup(func() {
		conn = prev
		_ = c.Close()
	})
	if _, err := c.Exec("CREATE TABLE kv (k TEXT PRIMARY KEY, v INTEGER NOT NULL)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := c.Exec("INSERT INTO kv VALUES ('a', 0), ('b', 0)"); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestRunInTx_StaleSnapshotRetriesWholeTx(t *testing.T) {
	// Arrange
	useTempWALConn(t)
	ctx := context.Background()
	runs := 0

	// Act
	err := RunInTx(ctx, func(tx *sql.Tx) error {
		runs++
		var v int
		if err := tx.QueryRowContext(ctx, "SELECT v FROM kv WHERE k='a'").Scan(&v); err != nil {
			return err
		}
		if runs == 1 {
			if _, err := conn.ExecContext(ctx, "UPDATE kv SET v = v + 10 WHERE k='b'"); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE kv SET v = v + 1 WHERE k='a'")
		return err
	})

	// Assert
	if err != nil {
		t.Fatalf("expected success via tx retry, got %v", err)
	}
	if runs != 2 {
		t.Errorf("fn runs = %d, want 2", runs)
	}
	var a, b int
	_ = conn.QueryRow("SELECT v FROM kv WHERE k='a'").Scan(&a)
	_ = conn.QueryRow("SELECT v FROM kv WHERE k='b'").Scan(&b)
	if a != 1 || b != 10 {
		t.Errorf("a=%d b=%d, want a=1 b=10", a, b)
	}
}

func TestRunInTx_ErrorHandling(t *testing.T) {
	tests := []struct {
		name     string
		fnErr    error
		wantRuns int
	}{
		{"non-lock error returns immediately", errors.New("boom"), 1},
		{"persistent lock error exhausts attempts", errTestLocked, txMaxAttempts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			useTempWALConn(t)
			runs := 0

			// Act
			err := RunInTx(context.Background(), func(tx *sql.Tx) error {
				runs++
				return tt.fnErr
			})

			// Assert
			if !errors.Is(err, tt.fnErr) {
				t.Errorf("err = %v, want %v", err, tt.fnErr)
			}
			if runs != tt.wantRuns {
				t.Errorf("fn runs = %d, want %d", runs, tt.wantRuns)
			}
		})
	}
}

func TestRunInTx_CtxCancelDuringBackoff(t *testing.T) {
	// Arrange
	useTempWALConn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := 0

	// Act
	start := time.Now()
	err := RunInTx(ctx, func(tx *sql.Tx) error {
		runs++
		time.AfterFunc(10*time.Millisecond, cancel)
		return errTestLocked
	})
	elapsed := time.Since(start)

	// Assert
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if runs != 1 {
		t.Errorf("fn runs = %d, want 1", runs)
	}
	if elapsed >= txRetryBaseDelay {
		t.Errorf("returned after %v, want < backoff %v", elapsed, txRetryBaseDelay)
	}
}
