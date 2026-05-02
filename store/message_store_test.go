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
