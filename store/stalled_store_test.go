package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
	"time"
)

func seedStalledTask(t *testing.T, email, task, requesterCanon, assigneeCanon string, daysAgo int) {
	t.Helper()
	src := testutil.RandomTS("stl")
	createdAt := time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	_, err := GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  requester, assignee, created_at, updated_at)
		 VALUES (?, ?, 'TASK', 'slack', 'general', ?, 0, 0, '{}', ?, ?, ?, '1970-01-01T00:00:00Z')`,
		email, task, src, requesterCanon, assigneeCanon, createdAt,
	)
	if err != nil {
		t.Fatalf("seedStalledTask: %v", err)
	}
}

func TestSelectStalled_MineBucket(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "alice@test.com"
	// D+4: older than threshold (3), Mine bucket (alice is requester)
	seedStalledTask(t, email, "Stalled mine task", email, "bob", 4)

	buckets, err := SelectStalled(context.Background(), email, 3)
	if err != nil {
		t.Fatalf("SelectStalled: %v", err)
	}
	if len(buckets.Mine) != 1 {
		t.Errorf("expected 1 Mine item, got %d", len(buckets.Mine))
	}
	if len(buckets.Observed) != 0 {
		t.Errorf("expected 0 Observed items, got %d", len(buckets.Observed))
	}
}

func TestSelectStalled_D2NotStalled(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "bob@test.com"
	// D+2: below threshold (3), should not appear
	seedStalledTask(t, email, "Recent task", email, "carol", 2)

	buckets, err := SelectStalled(context.Background(), email, 3)
	if err != nil {
		t.Fatalf("SelectStalled: %v", err)
	}
	if len(buckets.Mine)+len(buckets.Observed) != 0 {
		t.Errorf("expected no stalled items for D+2 task")
	}
}

func TestSelectStalled_ObservedBucket(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "carol@test.com"
	// X->Y task in same room: neither requester=carol nor assignee=carol
	seedStalledTask(t, email, "X to Y task", "xander", "yvonne", 5)

	buckets, err := SelectStalled(context.Background(), email, 3)
	if err != nil {
		t.Fatalf("SelectStalled: %v", err)
	}
	if len(buckets.Observed) != 1 {
		t.Errorf("expected 1 Observed item, got %d", len(buckets.Observed))
	}
	if len(buckets.Mine) != 0 {
		t.Errorf("expected 0 Mine items, got %d", len(buckets.Mine))
	}
}

func TestSelectStalled_DoneExcluded(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "dave@test.com"
	src := testutil.RandomTS("done")
	createdAt := time.Now().UTC().AddDate(0, 0, -5).Format(time.RFC3339)
	_, err = GetDB().Exec(
		`INSERT INTO messages (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  requester, assignee, created_at, updated_at)
		 VALUES (?, 'Done task', 'TASK', 'slack', 'general', ?, 1, 0, '{}', ?, 'eve', ?, '1970-01-01T00:00:00Z')`,
		email, src, email, createdAt,
	)
	if err != nil {
		t.Fatalf("seed done task: %v", err)
	}

	buckets, err := SelectStalled(context.Background(), email, 3)
	if err != nil {
		t.Fatalf("SelectStalled: %v", err)
	}
	if len(buckets.Mine)+len(buckets.Observed) != 0 {
		t.Errorf("expected no stalled items for done task")
	}
}
