package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
)

func TestGetMessageByIDPreservesDeadlineColdAndWarmCache(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup database: %v", err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	date := time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		date     sql.NullTime
		inferred sql.NullInt64
		wantDate string
		wantFlag bool
	}{
		{"explicit", sql.NullTime{Time: date, Valid: true}, sql.NullInt64{Valid: true}, "2026-11-04", false},
		{"inferred", sql.NullTime{Time: date, Valid: true}, sql.NullInt64{Int64: 1, Valid: true}, "2026-11-04", true},
		{"null", sql.NullTime{}, sql.NullInt64{}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email := testutil.RandomEmail("deadline-lookup")
			result, err := GetDB().ExecContext(ctx, `INSERT INTO messages
				(user_email, source, source_ts, task, deadline_date, deadline_inferred)
				VALUES (?, 'slack', ?, 'Deadline task', ?, ?)`, email, testutil.RandomTS(tc.name), tc.date, tc.inferred)
			if err != nil {
				t.Fatalf("insert message: %v", err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				t.Fatalf("message ID: %v", err)
			}
			cold, err := GetMessageByID(ctx, GetDB(), email, MessageID(id))
			if err != nil {
				t.Fatalf("cold lookup: %v", err)
			}
			if cold.DeadlineDate != tc.wantDate || cold.DeadlineInferred != tc.wantFlag {
				t.Errorf("cold deadline = (%q, %t), want (%q, %t)", cold.DeadlineDate, cold.DeadlineInferred, tc.wantDate, tc.wantFlag)
			}
			if err := RefreshCache(ctx, email); err != nil {
				t.Fatalf("refresh cache: %v", err)
			}
			warm, err := GetMessageByID(ctx, GetDB(), email, MessageID(id))
			if err != nil {
				t.Fatalf("warm lookup: %v", err)
			}
			if warm.DeadlineDate != tc.wantDate || warm.DeadlineInferred != tc.wantFlag {
				t.Errorf("warm deadline = (%q, %t), want (%q, %t)", warm.DeadlineDate, warm.DeadlineInferred, tc.wantDate, tc.wantFlag)
			}
			if cold.DeadlineDate != warm.DeadlineDate || cold.DeadlineInferred != warm.DeadlineInferred {
				t.Errorf("cold and warm deadlines differ: cold=(%q, %t), warm=(%q, %t)", cold.DeadlineDate, cold.DeadlineInferred, warm.DeadlineDate, warm.DeadlineInferred)
			}
		})
	}
}
