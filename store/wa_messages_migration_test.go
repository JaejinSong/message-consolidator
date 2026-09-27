package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// TestAddWAMessagesReplayColumns_IdempotentOnLegacyTable pins that the v23 migration adds
// raw_json/popped_at/processed_at/scan_attempts to a pre-v23 wa_messages table exactly
// once, and re-running it is a safe no-op (existing prod DBs replay this on every startup).
func TestAddWAMessagesReplayColumns_IdempotentOnLegacyTable(t *testing.T) {
	raw, err := sql.Open("sqlite", fmt.Sprintf("file:wareplaycols_%d?mode=memory", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	if _, err := raw.Exec(`CREATE TABLE wa_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id TEXT NOT NULL,
		email TEXT NOT NULL DEFAULT '',
		ts INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create legacy wa_messages table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO wa_messages (message_id, email) VALUES ('m1', 'u@test.com')`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	ctx := context.Background()
	if err := addWAMessagesReplayColumns(ctx, raw); err != nil {
		t.Fatalf("first run: %v", err)
	}

	for _, col := range []string{"raw_json", "popped_at", "processed_at", "scan_attempts"} {
		var has int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('wa_messages') WHERE name=?`, col).Scan(&has); err != nil {
			t.Fatalf("check column %s: %v", col, err)
		}
		if has == 0 {
			t.Errorf("expected column %s to exist after migration", col)
		}
	}

	var rawJSON string
	var attempts int64
	if err := raw.QueryRow(`SELECT raw_json, scan_attempts FROM wa_messages WHERE message_id = 'm1'`).Scan(&rawJSON, &attempts); err != nil {
		t.Fatalf("scan defaults: %v", err)
	}
	if rawJSON != "" || attempts != 0 {
		t.Errorf("expected defaults raw_json='' scan_attempts=0, got raw_json=%q scan_attempts=%d", rawJSON, attempts)
	}

	// Why: re-running must not error or duplicate columns (SQLite rejects a second
	// ALTER TABLE ADD COLUMN with the same name).
	if err := addWAMessagesReplayColumns(ctx, raw); err != nil {
		t.Fatalf("second run should be a no-op: %v", err)
	}
}
