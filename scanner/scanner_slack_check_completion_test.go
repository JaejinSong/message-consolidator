package scanner

import (
	"context"
	"strings"
	"testing"

	"message-consolidator/store"
)

// insertThreadTask inserts a minimal messages row tied to a Slack thread so
// GetIncompleteByThreadID can find it.
func insertThreadTask(ctx context.Context, t *testing.T, email, threadID string, done bool) store.MessageID {
	t.Helper()
	doneInt := 0
	if done {
		doneInt = 1
	}
	res, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, created_at)
		 VALUES (?, 'slack', 'room', 'still open task', ?, ?, 0, datetime('now'))`,
		email, threadID, doneInt)
	if err != nil {
		t.Fatalf("insertThreadTask: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return store.MessageID(id)
}

func readMessageMetadata(ctx context.Context, t *testing.T, id store.MessageID) string {
	t.Helper()
	var metadata string
	row := store.GetDB().QueryRowContext(ctx, `SELECT COALESCE(metadata, '') FROM messages WHERE id = ?`, int64(id))
	if err := row.Scan(&metadata); err != nil {
		t.Fatalf("scan metadata: %v", err)
	}
	return metadata
}

// TestProposeThreadCheckCompletion_OpenTaskGetsCandidate verifies a still-open task in
// the checked thread receives a pending completion candidate keyed by the thread.
func TestProposeThreadCheckCompletion_OpenTaskGetsCandidate(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	id := insertThreadTask(ctx, t, "check@example.com", "100.000000", false)

	proposeThreadCheckCompletion(ctx, store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "100.000000", UserEmail: "check@example.com",
	})

	metadata := readMessageMetadata(ctx, t, id)
	if !strings.Contains(metadata, `"status":"pending"`) {
		t.Errorf("expected a pending completion candidate, got metadata=%s", metadata)
	}
	if !strings.Contains(metadata, "slack-check:C1:100.000000") {
		t.Errorf("expected source_link to key the thread check, got metadata=%s", metadata)
	}
}

// TestProposeThreadCheckCompletion_DoneTaskSkipped verifies a task that is already done
// is not returned by GetIncompleteByThreadID and so never gets a candidate.
func TestProposeThreadCheckCompletion_DoneTaskSkipped(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	id := insertThreadTask(ctx, t, "check2@example.com", "200.000000", true)

	proposeThreadCheckCompletion(ctx, store.SlackThreadMeta{
		ChannelID: "C2", ThreadTS: "200.000000", UserEmail: "check2@example.com",
	})

	metadata := readMessageMetadata(ctx, t, id)
	if strings.Contains(metadata, "completion_candidate") {
		t.Errorf("done task must not receive a completion candidate, got metadata=%s", metadata)
	}
}

// TestProposeThreadCheckCompletion_DismissedKeySkipped verifies a task whose thread-check
// source key was already dismissed does not get a new candidate recorded.
func TestProposeThreadCheckCompletion_DismissedKeySkipped(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	id := insertThreadTask(ctx, t, "check3@example.com", "300.000000", false)
	sourceKey := "slack-check:C3:300.000000"
	if _, err := store.GetDB().ExecContext(ctx,
		`UPDATE messages SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.completion_dismissed_source', ?) WHERE id = ?`,
		sourceKey, int64(id)); err != nil {
		t.Fatalf("seed dismissed marker: %v", err)
	}

	proposeThreadCheckCompletion(ctx, store.SlackThreadMeta{
		ChannelID: "C3", ThreadTS: "300.000000", UserEmail: "check3@example.com",
	})

	metadata := readMessageMetadata(ctx, t, id)
	if strings.Contains(metadata, "completion_candidate") {
		t.Errorf("previously dismissed source key must not be re-recorded, got metadata=%s", metadata)
	}
}
