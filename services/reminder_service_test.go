package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"strings"
	"testing"
	"time"
)

type fakeSlack struct {
	sent []string // slackUserID|text pairs
	err  error
}

func (f *fakeSlack) SendDM(_ context.Context, slackUserID, text string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, slackUserID+"|"+text)
	return nil
}

func setupReminderTestDB(t *testing.T) func() {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	return cleanup
}

// seedDueMessage inserts a message with the given deadline and metadata.
// Returns the inserted row's id.
func seedDueMessage(t *testing.T, email, task, deadline, metadata string) int64 {
	t.Helper()
	src := testutil.RandomTS("src")
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, source, room, source_ts, done, is_deleted, deadline, metadata)
		 VALUES (?, ?, 'slack', 'general', ?, 0, 0, ?, ?)`,
		email, task, src, deadline, metadata,
	)
	if err != nil {
		t.Fatalf("seedDueMessage: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedUserWithSlack inserts a user directly into the DB with a slack_id set,
// then invalidates the user cache so GetOrCreateUser will re-load from DB.
func seedUserWithSlack(t *testing.T, email, slackID string) {
	t.Helper()
	_, err := store.GetDB().Exec(
		`INSERT OR IGNORE INTO users (email, name) VALUES (?, ?)`,
		email, "Test User",
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, err = store.GetDB().Exec(
		`UPDATE users SET slack_id = ? WHERE email = ?`,
		slackID, email,
	)
	if err != nil {
		t.Fatalf("update slack_id: %v", err)
	}
	store.InvalidateAllUsersCache()
	// Evict per-user cache entry by calling GetOrCreateUser which will re-load from DB.
	// Since cache now reflects DB, subsequent GetOrCreateUser hits will have SlackID set.
}

// TestReminderService_WindowBoundary verifies that only messages within the ±10 min window
// are dispatched and messages outside the boundary are skipped.
func TestReminderService_WindowBoundary(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("boundary")
	seedUserWithSlack(t, email, "UBOUNDARY")

	now := time.Now().UTC()
	inWindow := now.Add(24 * time.Hour).Format(time.RFC3339)
	outOfWindow := now.Add(48 * time.Hour).Format(time.RFC3339)

	seedDueMessage(t, email, "Task In Window", inWindow, "")
	seedDueMessage(t, email, "Task Out Of Window", outOfWindow, "")

	fs := &fakeSlack{}
	svc := NewReminderService(fs, []int{24})

	if err := svc.DispatchDueSoon(ctx); err != nil {
		t.Fatalf("DispatchDueSoon: %v", err)
	}

	if len(fs.sent) != 1 {
		t.Errorf("expected 1 DM sent, got %d: %v", len(fs.sent), fs.sent)
	}
}

// TestReminderService_SkipNoSlackID verifies that users without a slack_id are silently skipped.
func TestReminderService_SkipNoSlackID(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("noslack")
	// Create user but do NOT set SlackID
	if _, err := store.GetOrCreateUser(ctx, email, "No Slack User", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now().UTC()
	deadline := now.Add(24 * time.Hour).Format(time.RFC3339)
	seedDueMessage(t, email, "Due Task", deadline, "")

	fs := &fakeSlack{}
	svc := NewReminderService(fs, []int{24})

	if err := svc.DispatchDueSoon(ctx); err != nil {
		t.Fatalf("DispatchDueSoon: %v", err)
	}

	if len(fs.sent) != 0 {
		t.Errorf("expected 0 DMs (no slack_id), got %d", len(fs.sent))
	}
}

// TestReminderService_SkipAlreadyReminded verifies that a message already marked with
// reminded_at_24h is not sent again.
func TestReminderService_SkipAlreadyReminded(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("reminded")
	seedUserWithSlack(t, email, "UREMINDED")

	already := map[string]any{
		"reminded_at_24h": time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339),
	}
	metaBytes, _ := json.Marshal(already)

	now := time.Now().UTC()
	deadline := now.Add(24 * time.Hour).Format(time.RFC3339)
	seedDueMessage(t, email, "Already Reminded Task", deadline, string(metaBytes))

	fs := &fakeSlack{}
	svc := NewReminderService(fs, []int{24})

	if err := svc.DispatchDueSoon(ctx); err != nil {
		t.Fatalf("DispatchDueSoon: %v", err)
	}

	if len(fs.sent) != 0 {
		t.Errorf("expected 0 DMs (already reminded), got %d", len(fs.sent))
	}
}

// TestReminderService_SendDMErrorSkipsMarkReminded verifies that when SendDM returns an error,
// the metadata is NOT updated (so the reminder is retried on the next tick).
func TestReminderService_SendDMErrorSkipsMarkReminded(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("dmerr")
	seedUserWithSlack(t, email, "UDMERR")

	now := time.Now().UTC()
	deadline := now.Add(24 * time.Hour).Format(time.RFC3339)
	msgID := seedDueMessage(t, email, "DM Error Task", deadline, "")

	fs := &fakeSlack{err: fmt.Errorf("slack API unavailable")}
	svc := NewReminderService(fs, []int{24})

	if err := svc.DispatchDueSoon(ctx); err != nil {
		t.Fatalf("DispatchDueSoon: %v", err)
	}

	// Verify metadata was NOT updated
	rows, err := store.SelectDueSoon(ctx,
		now.Add(23*time.Hour+50*time.Minute).Format(time.RFC3339),
		now.Add(24*time.Hour+10*time.Minute).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("SelectDueSoon: %v", err)
	}
	var found store.DueSoonMessage
	for _, r := range rows {
		if r.ID == store.MessageID(msgID) {
			found = r
			break
		}
	}
	if store.HasReminded(found.Metadata, "24h") {
		t.Error("expected metadata NOT to have reminded_at_24h after SendDM error")
	}
}

// seedUndatedCommitment inserts a PROMISE/WAITING row with no deadline, aged by daysAgo.
func seedUndatedCommitment(t *testing.T, email, task, category string, daysAgo int) int64 {
	t.Helper()
	src := testutil.RandomTS("und")
	createdAt := time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, category, source, room, source_ts, done, is_deleted, metadata, created_at, assignee, requester)
		 VALUES (?, ?, ?, 'slack', 'general', ?, 0, 0, '{}', ?, 'alice', 'bob')`,
		email, task, category, src, createdAt,
	)
	if err != nil {
		t.Fatalf("seedUndatedCommitment: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestDispatchUndated_D3SendsOnce(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("und")
	seedUserWithSlack(t, email, "UUND1")
	seedUndatedCommitment(t, email, "Write report", "PROMISE", 3)

	fs := &fakeSlack{}
	svc := NewReminderService(fs, nil)

	if err := svc.DispatchUndated(ctx); err != nil {
		t.Fatalf("DispatchUndated: %v", err)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("expected 1 DM sent, got %d", len(fs.sent))
	}
}

func TestDispatchUndated_D2NoSend(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("und2")
	seedUserWithSlack(t, email, "UUND2")
	seedUndatedCommitment(t, email, "Too early", "PROMISE", 2)

	fs := &fakeSlack{}
	svc := NewReminderService(fs, nil)

	if err := svc.DispatchUndated(ctx); err != nil {
		t.Fatalf("DispatchUndated: %v", err)
	}
	if len(fs.sent) != 0 {
		t.Errorf("expected 0 DMs sent, got %d", len(fs.sent))
	}
}

func TestDispatchUndated_AlreadyMarkedSkips(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("undm")
	seedUserWithSlack(t, email, "UUNDM")

	// Pre-mark d3 so it should be skipped
	meta := `{"reminded_at_undated_d3":"2026-01-01T00:00:00Z"}`
	seedUndatedCommitment_withMeta(t, email, "Already done", "PROMISE", 5, meta)

	fs := &fakeSlack{}
	svc := NewReminderService(fs, nil)

	if err := svc.DispatchUndated(ctx); err != nil {
		t.Fatalf("DispatchUndated: %v", err)
	}
	if len(fs.sent) != 0 {
		t.Errorf("expected 0 DMs (already marked), got %d", len(fs.sent))
	}
}

func seedUndatedCommitment_withMeta(t *testing.T, email, task, category string, daysAgo int, meta string) int64 {
	t.Helper()
	src := testutil.RandomTS("undm")
	createdAt := time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, category, source, room, source_ts, done, is_deleted, metadata, created_at, assignee, requester)
		 VALUES (?, ?, ?, 'slack', 'general', ?, 0, 0, ?, ?, 'alice', 'bob')`,
		email, task, category, src, meta, createdAt,
	)
	if err != nil {
		t.Fatalf("seedUndatedCommitment_withMeta: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedStalledTask inserts an open TASK row whose created_at/updated_at are both
// backdated by daysAgo, simulating a task with no activity since creation.
func seedStalledTask(t *testing.T, email, task string, daysAgo int, metadata string) int64 {
	t.Helper()
	src := testutil.RandomTS("stalled")
	ts := time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, category, source, room, source_ts, done, is_deleted, metadata, created_at, updated_at, assignee, requester)
		 VALUES (?, ?, 'TASK', 'slack', 'general', ?, 0, 0, ?, ?, ?, 'bob', ?)`,
		email, task, src, metadata, ts, ts, email,
	)
	if err != nil {
		t.Fatalf("seedStalledTask: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// TestDispatchStalledReconfirm_DigestBothThresholds verifies a D+14 and a D+30 task
// are combined into a single digest DM (one per user), each marked at its own
// crossed threshold, a D+12 task is excluded, and a second dispatch sends nothing.
func TestDispatchStalledReconfirm_DigestBothThresholds(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("stalled")
	seedUserWithSlack(t, email, "USTALLED")

	seedStalledTask(t, email, "D14 task", 14, "{}")
	seedStalledTask(t, email, "D30 task", 30, "{}")
	seedStalledTask(t, email, "D12 task", 12, "{}")

	fs := &fakeSlack{}
	svc := NewReminderService(fs, nil)

	if err := svc.DispatchStalledReconfirm(ctx); err != nil {
		t.Fatalf("DispatchStalledReconfirm: %v", err)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("expected 1 digest DM sent, got %d: %v", len(fs.sent), fs.sent)
	}
	digest := fs.sent[0]
	if !strings.Contains(digest, "D14 task") || !strings.Contains(digest, "D30 task") {
		t.Errorf("digest missing expected tasks: %s", digest)
	}
	if strings.Contains(digest, "D12 task") {
		t.Errorf("digest should exclude D+12 task: %s", digest)
	}

	buckets, err := store.SelectStalled(ctx, email, 1)
	if err != nil {
		t.Fatalf("SelectStalled: %v", err)
	}
	for _, item := range buckets.Mine {
		switch item.Task {
		case "D14 task":
			if !store.HasReminded(item.Metadata, "stalled_reconfirm_d13") {
				t.Error("D14 task should be marked stalled_reconfirm_d13")
			}
		case "D30 task":
			if !store.HasReminded(item.Metadata, "stalled_reconfirm_d29") {
				t.Error("D30 task should be marked stalled_reconfirm_d29")
			}
		}
	}

	// Second dispatch: everything already reminded (or below threshold) — no new DM.
	if err := svc.DispatchStalledReconfirm(ctx); err != nil {
		t.Fatalf("DispatchStalledReconfirm (2nd): %v", err)
	}
	if len(fs.sent) != 1 {
		t.Errorf("expected no additional DM on 2nd dispatch, got %d total: %v", len(fs.sent), fs.sent)
	}
}

func TestDispatchStalledReconfirm_SkipNoSlackID(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("stalledns")
	if _, err := store.GetOrCreateUser(ctx, email, "No Slack User", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	seedStalledTask(t, email, "D14 task", 14, "{}")

	fs := &fakeSlack{}
	svc := NewReminderService(fs, nil)

	if err := svc.DispatchStalledReconfirm(ctx); err != nil {
		t.Fatalf("DispatchStalledReconfirm: %v", err)
	}
	if len(fs.sent) != 0 {
		t.Errorf("expected 0 DMs (no slack_id), got %d", len(fs.sent))
	}
}

func TestDispatchUndated_SendDMErrorNoMark(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("underr")
	seedUserWithSlack(t, email, "UUNDERR")
	seedUndatedCommitment(t, email, "Fail task", "PROMISE", 4)

	fs := &fakeSlack{err: fmt.Errorf("slack down")}
	svc := NewReminderService(fs, nil)

	if err := svc.DispatchUndated(ctx); err != nil {
		t.Fatalf("DispatchUndated: %v", err)
	}

	rows, err := store.SelectUndated(ctx)
	if err != nil {
		t.Fatalf("SelectUndated: %v", err)
	}
	for _, r := range rows {
		if r.UserEmail == email {
			var m map[string]json.RawMessage
			_ = json.Unmarshal([]byte(r.Metadata), &m)
			if _, found := m["reminded_at_undated_d3"]; found {
				t.Error("metadata should NOT have undated_d3 after SendDM failure")
			}
		}
	}
}
