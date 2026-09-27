package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

// insertSlackTask inserts a minimal messages row with a Slack thread_id, lifecycle
// derived from done/is_deleted, and assigned_at offset from now by ageDays.
func insertSlackTask(ctx context.Context, t *testing.T, email, threadID string, ageDays int, done bool) {
	t.Helper()
	doneInt := 0
	if done {
		doneInt = 1
	}
	_, err := GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, assigned_at, created_at)
		 VALUES (?, 'slack', 'room', 'task', ?, ?, 0, datetime('now', printf('-%d days', ?)), datetime('now'))`,
		email, threadID, doneInt, ageDays)
	if err != nil {
		t.Fatalf("insertSlackTask: %v", err)
	}
}

func insertSlackThreadRow(ctx context.Context, t *testing.T, channelID, threadTS, status, email string) {
	t.Helper()
	_, err := GetDB().ExecContext(ctx,
		`INSERT INTO slack_threads (channel_id, thread_ts, last_reply_ts, last_activity_ts, status, user_email)
		 VALUES (?, ?, '', '', ?, ?)`,
		channelID, threadTS, status, email)
	if err != nil {
		t.Fatalf("insertSlackThreadRow: %v", err)
	}
}

func TestGetColdReconciliationThreads(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	defer cleanup()
	ctx := context.Background()

	// resolved thread whose task is still active → selected.
	insertSlackThreadRow(ctx, t, "C_RESOLVED", "111.000000", "resolved", "u1@test.com")
	insertSlackTask(ctx, t, "u1@test.com", "111.000000", 5, false)

	// resolved thread whose task is done → not selected.
	insertSlackThreadRow(ctx, t, "C_DONE", "222.000000", "resolved", "u2@test.com")
	insertSlackTask(ctx, t, "u2@test.com", "222.000000", 5, true)

	// active-status thread (hot sweep already covers it) → not selected even though task is active.
	insertSlackThreadRow(ctx, t, "C_ACTIVE", "333.000000", "active", "u3@test.com")
	insertSlackTask(ctx, t, "u3@test.com", "333.000000", 5, false)

	// resolved thread whose task is older than 61 days → not selected.
	insertSlackThreadRow(ctx, t, "C_OLD", "444.000000", "resolved", "u4@test.com")
	insertSlackTask(ctx, t, "u4@test.com", "444.000000", 90, false)

	got, err := GetColdReconciliationThreads(ctx)
	if err != nil {
		t.Fatalf("GetColdReconciliationThreads: %v", err)
	}

	byThreadTS := make(map[string]SlackThreadMeta, len(got))
	for _, th := range got {
		byThreadTS[th.ThreadTS] = th
	}

	if th, ok := byThreadTS["111.000000"]; !ok {
		t.Error("expected resolved thread with active task to be selected")
	} else if th.ChannelID != "C_RESOLVED" {
		t.Errorf("channel_id = %q, want %q", th.ChannelID, "C_RESOLVED")
	}
	if _, ok := byThreadTS["222.000000"]; ok {
		t.Error("thread whose task is done must not be selected")
	}
	if _, ok := byThreadTS["333.000000"]; ok {
		t.Error("active-status thread must not be selected (hot sweep already covers it)")
	}
	if _, ok := byThreadTS["444.000000"]; ok {
		t.Error("task older than 61 days must not be selected")
	}
}

func TestTouchSlackThreadTimestamps_DoesNotChangeStatus(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	defer cleanup()
	ctx := context.Background()

	insertSlackThreadRow(ctx, t, "C1", "555.000000", "resolved", "u5@test.com")

	if err := TouchSlackThreadTimestamps(ctx, "C1", "555.000000", "600.000000", "600.000000", "u5@test.com"); err != nil {
		t.Fatalf("TouchSlackThreadTimestamps: %v", err)
	}

	var status, lastReplyTS string
	row := GetDB().QueryRowContext(ctx, `SELECT status, last_reply_ts FROM slack_threads WHERE channel_id = ? AND thread_ts = ?`, "C1", "555.000000")
	if err := row.Scan(&status, &lastReplyTS); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "resolved" {
		t.Errorf("status = %q, want unchanged %q", status, "resolved")
	}
	if lastReplyTS != "600.000000" {
		t.Errorf("last_reply_ts = %q, want %q", lastReplyTS, "600.000000")
	}
}
