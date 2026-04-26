package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
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
