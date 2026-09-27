package store

import (
	"context"
	"fmt"
	"message-consolidator/internal/testutil"
	"testing"
	"time"
)

func setupWAReplayTest(t *testing.T) (string, func()) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	return testutil.RandomEmail("wa-replay"), cleanup
}

// insertRawWAMessage bypasses InsertWAMessage so the test can control created_at,
// popped_at, processed_at and scan_attempts directly instead of relying on defaults.
func insertRawWAMessage(t *testing.T, email, messageID, rawJSON string, ts int64, createdAt string, poppedAt, processedAt *string, scanAttempts int64) {
	t.Helper()
	_, err := GetDB().ExecContext(context.Background(), `
		INSERT INTO wa_messages
			(message_id, email, chat_jid, ts, raw_json, created_at, popped_at, processed_at, scan_attempts)
		VALUES (?, ?, 'chat-1', ?, ?, ?, ?, ?, ?)`,
		messageID, email, ts, rawJSON, createdAt, poppedAt, processedAt, scanAttempts)
	if err != nil {
		t.Fatalf("insert raw wa_message %s: %v", messageID, err)
	}
}

func TestListReplayableWAMessages_FiltersEachDimension(t *testing.T) {
	email, cleanup := setupWAReplayTest(t)
	defer cleanup()

	now := time.Now().UTC()
	old := now.Add(-waCreatedGrace - time.Minute).Format(sqliteDatetimeFormat)
	tooRecent := now.Add(-time.Minute).Format(sqliteDatetimeFormat)
	beyondLookback := now.Add(-waLookback - time.Hour).Unix()
	withinLookback := now.Add(-time.Hour).Unix()
	stalePop := now.Add(-waPoppedStale - time.Minute).Format(sqliteDatetimeFormat)
	freshPop := now.Add(-time.Minute).Format(sqliteDatetimeFormat)

	// Eligible: settled, has payload, under attempt cap, no live pop, within lookback.
	insertRawWAMessage(t, email, "eligible", `{"id":"eligible"}`, withinLookback, old, nil, nil, 0)
	// Excluded: already processed.
	processedAt := old
	insertRawWAMessage(t, email, "processed", `{"id":"processed"}`, withinLookback, old, nil, &processedAt, 0)
	// Excluded: no captured payload.
	insertRawWAMessage(t, email, "empty-raw", "", withinLookback, old, nil, nil, 0)
	// Excluded: exhausted retry cap.
	insertRawWAMessage(t, email, "exhausted", `{"id":"exhausted"}`, withinLookback, old, nil, nil, waMaxScanAttempts)
	// Excluded: inserted too recently (write-settle grace).
	insertRawWAMessage(t, email, "too-recent", `{"id":"too-recent"}`, withinLookback, tooRecent, nil, nil, 0)
	// Excluded: older than the lookback window.
	insertRawWAMessage(t, email, "ancient", `{"id":"ancient"}`, beyondLookback, old, nil, nil, 0)
	// Excluded: held by a live (non-stale) pop.
	insertRawWAMessage(t, email, "live-pop", `{"id":"live-pop"}`, withinLookback, old, &freshPop, nil, 0)
	// Eligible: pop lock is stale (crashed worker), treated as abandoned.
	insertRawWAMessage(t, email, "stale-pop", `{"id":"stale-pop"}`, withinLookback, old, &stalePop, nil, 0)

	got, err := ListReplayableWAMessages(context.Background(), email, now)
	if err != nil {
		t.Fatalf("ListReplayableWAMessages: %v", err)
	}

	ids := make(map[string]bool, len(got))
	for _, r := range got {
		ids[r.MessageID] = true
	}
	if !ids["eligible"] || !ids["stale-pop"] {
		t.Errorf("expected eligible and stale-pop rows to be returned, got: %+v", got)
	}
	for _, excluded := range []string{"processed", "empty-raw", "exhausted", "too-recent", "ancient", "live-pop"} {
		if ids[excluded] {
			t.Errorf("expected %s to be filtered out, got: %+v", excluded, got)
		}
	}
}

func TestMarkWAMessages_PoppedProcessedFailed(t *testing.T) {
	email, cleanup := setupWAReplayTest(t)
	defer cleanup()

	now := time.Now().UTC()
	old := now.Add(-waCreatedGrace - time.Minute).Format(sqliteDatetimeFormat)
	insertRawWAMessage(t, email, "msg-1", `{"id":"msg-1"}`, now.Unix(), old, nil, nil, 0)

	// Why: every pop must count as an attempt so a crash/panic between pop and ack
	// still ages the row out, instead of relying on an ack that may never come.
	if err := MarkWAMessagesPopped(context.Background(), email, []string{"msg-1"}); err != nil {
		t.Fatalf("MarkWAMessagesPopped: %v", err)
	}
	var poppedAt *string
	if err := GetDB().QueryRow(`SELECT popped_at FROM wa_messages WHERE message_id = 'msg-1'`).Scan(&poppedAt); err != nil {
		t.Fatalf("scan popped_at: %v", err)
	}
	if poppedAt == nil {
		t.Fatal("expected popped_at to be set after MarkWAMessagesPopped")
	}
	var attempts int64
	if err := GetDB().QueryRow(`SELECT scan_attempts FROM wa_messages WHERE message_id = 'msg-1'`).Scan(&attempts); err != nil {
		t.Fatalf("scan scan_attempts: %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected scan_attempts = 1 after MarkWAMessagesPopped, got %d", attempts)
	}

	if err := MarkWAMessagesFailed(context.Background(), email, []string{"msg-1"}); err != nil {
		t.Fatalf("MarkWAMessagesFailed: %v", err)
	}
	if err := GetDB().QueryRow(`SELECT scan_attempts FROM wa_messages WHERE message_id = 'msg-1'`).Scan(&attempts); err != nil {
		t.Fatalf("scan scan_attempts: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected scan_attempts = 2 after MarkWAMessagesFailed, got %d", attempts)
	}

	if err := MarkWAMessagesProcessed(context.Background(), email, []string{"msg-1"}); err != nil {
		t.Fatalf("MarkWAMessagesProcessed: %v", err)
	}
	var processedAt *string
	if err := GetDB().QueryRow(`SELECT processed_at FROM wa_messages WHERE message_id = 'msg-1'`).Scan(&processedAt); err != nil {
		t.Fatalf("scan processed_at: %v", err)
	}
	if processedAt == nil {
		t.Fatal("expected processed_at to be set after MarkWAMessagesProcessed")
	}

	// Why: once processed, MarkWAMessagesFailed and MarkWAMessagesPopped must be no-ops
	// (processed_at IS NULL guard) so a late-arriving retry cannot resurrect a finished row.
	if err := MarkWAMessagesFailed(context.Background(), email, []string{"msg-1"}); err != nil {
		t.Fatalf("MarkWAMessagesFailed after processed: %v", err)
	}
	if err := GetDB().QueryRow(`SELECT scan_attempts FROM wa_messages WHERE message_id = 'msg-1'`).Scan(&attempts); err != nil {
		t.Fatalf("scan scan_attempts: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected scan_attempts to stay at 2 after processed-row retry, got %d", attempts)
	}
}

// TestMarkWAMessagesPopped_UnackedRowAgesOutViaAttemptCap covers the bug this test
// guards against: a pop that never gets acked (panic, early return) must still burn
// down the retry cap via popped_at's own scan_attempts bump, not loop forever.
func TestMarkWAMessagesPopped_UnackedRowAgesOutViaAttemptCap(t *testing.T) {
	email, cleanup := setupWAReplayTest(t)
	defer cleanup()

	now := time.Now().UTC()
	old := now.Add(-waCreatedGrace - time.Minute).Format(sqliteDatetimeFormat)
	stalePop := now.Add(-waPoppedStale - time.Minute).Format(sqliteDatetimeFormat)
	insertRawWAMessage(t, email, "never-acked", `{"id":"never-acked"}`, now.Unix(), old, nil, nil, 0)

	for i := 0; i < waMaxScanAttempts; i++ {
		if err := MarkWAMessagesPopped(context.Background(), email, []string{"never-acked"}); err != nil {
			t.Fatalf("MarkWAMessagesPopped iteration %d: %v", i, err)
		}
		// Why: simulate the pop lock going stale (crashed worker) between attempts so
		// ListReplayableWAMessages would otherwise consider the row eligible again.
		if _, err := GetDB().ExecContext(context.Background(),
			`UPDATE wa_messages SET popped_at = ? WHERE message_id = 'never-acked'`, stalePop); err != nil {
			t.Fatalf("force stale pop iteration %d: %v", i, err)
		}
	}

	var attempts int64
	if err := GetDB().QueryRow(`SELECT scan_attempts FROM wa_messages WHERE message_id = 'never-acked'`).Scan(&attempts); err != nil {
		t.Fatalf("scan scan_attempts: %v", err)
	}
	if attempts != waMaxScanAttempts {
		t.Fatalf("expected scan_attempts = %d after %d unacked pops, got %d", waMaxScanAttempts, waMaxScanAttempts, attempts)
	}

	got, err := ListReplayableWAMessages(context.Background(), email, now)
	if err != nil {
		t.Fatalf("ListReplayableWAMessages: %v", err)
	}
	for _, r := range got {
		if r.MessageID == "never-acked" {
			t.Fatalf("expected never-acked to be excluded once scan_attempts reaches the cap, got: %+v", got)
		}
	}
}

func TestMarkWAMessages_EmptyIDsNoop(t *testing.T) {
	_, cleanup := setupWAReplayTest(t)
	defer cleanup()

	if err := MarkWAMessagesPopped(context.Background(), "any@test.com", nil); err != nil {
		t.Fatalf("MarkWAMessagesPopped with empty ids: %v", err)
	}
	if err := MarkWAMessagesProcessed(context.Background(), "any@test.com", []string{}); err != nil {
		t.Fatalf("MarkWAMessagesProcessed with empty ids: %v", err)
	}
	if err := MarkWAMessagesFailed(context.Background(), "any@test.com", nil); err != nil {
		t.Fatalf("MarkWAMessagesFailed with empty ids: %v", err)
	}
}

func TestUpdateWAMessagesByID_ChunksOverSQLiteLimit(t *testing.T) {
	email, cleanup := setupWAReplayTest(t)
	defer cleanup()

	now := time.Now().UTC()
	old := now.Add(-waCreatedGrace - time.Minute).Format(sqliteDatetimeFormat)
	total := waSQLiteInChunk + 5
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("chunk-msg-%d", i)
		ids = append(ids, id)
		insertRawWAMessage(t, email, id, `{"id":"x"}`, now.Unix(), old, nil, nil, 0)
	}

	if err := MarkWAMessagesProcessed(context.Background(), email, ids); err != nil {
		t.Fatalf("MarkWAMessagesProcessed over chunk boundary: %v", err)
	}

	var processedCount int
	if err := GetDB().QueryRow(`SELECT COUNT(*) FROM wa_messages WHERE email = ? AND processed_at IS NOT NULL`, email).Scan(&processedCount); err != nil {
		t.Fatalf("count processed: %v", err)
	}
	if processedCount != total {
		t.Errorf("expected all %d rows processed across chunk boundary, got %d", total, processedCount)
	}
}
