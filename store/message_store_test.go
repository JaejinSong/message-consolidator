package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

// Why (R): dominance gating — single dominant actor (≥2 count, ≥50%) returns ok=true,
//
//	mixed/sparse rooms return ok=false to skip the fallback.
func TestGetRoomDefaultActor_DominanceGating(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "user@test"
	room := "biz-test"

	// Insert a dominant actor pattern: Phathit ×3, others ×1
	insertActorSample(ctx, t, email, room, "requester1", "Phathit")
	insertActorSample(ctx, t, email, room, "requester2", "Phathit")
	insertActorSample(ctx, t, email, room, "requester3", "Phathit")
	insertActorSample(ctx, t, email, room, "requester4", "Other")

	actor, ok := GetRoomDefaultActor(ctx, email, room)
	if !ok || actor != "Phathit" {
		t.Errorf("dominant case: got (%q,%v), want (\"Phathit\", true)", actor, ok)
	}

	// Sparse room — single actor with count=1 → not dominant enough
	sparseRoom := "biz-sparse"
	insertActorSample(ctx, t, email, sparseRoom, "req", "OnlyOne")
	if _, ok := GetRoomDefaultActor(ctx, email, sparseRoom); ok {
		t.Error("sparse room: expected ok=false (count<2)")
	}

	// Mixed room — no dominance (50-50)
	mixed := "biz-mixed"
	insertActorSample(ctx, t, email, mixed, "r1", "A")
	insertActorSample(ctx, t, email, mixed, "r2", "A")
	insertActorSample(ctx, t, email, mixed, "r3", "B")
	insertActorSample(ctx, t, email, mixed, "r4", "B")
	if _, ok := GetRoomDefaultActor(ctx, email, mixed); ok {
		t.Error("mixed room: expected ok=false (no dominance)")
	}
}

// Why: helper for actor-frequency fixtures; minimal columns sufficient for the GROUP BY query.
func insertActorSample(ctx context.Context, t *testing.T, email, room, requester, assignee string) {
	t.Helper()
	_, _ = GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, requester, assignee, is_deleted, created_at)
		 VALUES (?, 'slack', ?, 't', ?, ?, 0, datetime('now'))`,
		email, room, requester, assignee)
}

func TestIsProcessed_UnknownMessage(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	ok, err := IsProcessed(ctx, GetDB(), "user@example.com", "ts-nonexistent")
	if err != nil {
		t.Fatalf("IsProcessed: %v", err)
	}
	if ok {
		t.Error("expected false for unknown sourceTS")
	}
}

func TestMarkAsProcessed_Then_IsProcessed(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "mark@example.com"
	ts := "ts-mark-test"

	if err := MarkAsProcessed(ctx, GetDB(), email, ts); err != nil {
		t.Fatalf("MarkAsProcessed: %v", err)
	}

	ok, err := IsProcessed(ctx, GetDB(), email, ts)
	if err != nil {
		t.Fatalf("IsProcessed after mark: %v", err)
	}
	if !ok {
		t.Error("expected true after MarkAsProcessed")
	}
}

func TestDeduplicateTasks_Empty(t *testing.T) {
	t.Parallel()
	if got := DeduplicateTasks(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
	if got := DeduplicateTasks([]TodoItem{}); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestDeduplicateTasks_NoDuplicates(t *testing.T) {
	t.Parallel()
	items := []TodoItem{
		{Task: "Fix login bug", SourceTS: "ts1"},
		{Task: "Update readme file", SourceTS: "ts2"},
	}
	got := DeduplicateTasks(items)
	if len(got) != 2 {
		t.Errorf("expected 2 items, got %d", len(got))
	}
}

func TestDeduplicateTasks_WithDuplicate(t *testing.T) {
	t.Parallel()
	items := []TodoItem{
		{Task: "Fix login bug", SourceTS: "ts1"},
		{Task: "Fix the login bug", SourceTS: "ts1"},
	}
	got := DeduplicateTasks(items)
	if len(got) != 1 {
		t.Errorf("expected 1 after dedup, got %d: %v", len(got), got)
	}
}

func TestUpdateTaskAssigneesBatch_Empty(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	// Empty updates map should be a no-op without error.
	if err := UpdateTaskAssigneesBatch(context.Background(), "u@example.com", map[MessageID]string{}); err != nil {
		t.Errorf("UpdateTaskAssigneesBatch empty: %v", err)
	}
}

func TestUpdateSubtasks_NoRows(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	// Message ID 999 does not exist; should not panic.
	err = UpdateSubtasks(context.Background(), GetDB(), "u@example.com", MessageID(999999), nil)
	if err != nil {
		t.Logf("UpdateSubtasks unknown id: %v (acceptable)", err)
	}
}
