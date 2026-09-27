package store

import (
	"context"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
)

// seedPastEventTask inserts a TASK row with the given deadline_date (YYYY-MM-DD) and
// metadata, honoring done/isDeleted so lifecycle filtering can be exercised.
func seedPastEventTask(t *testing.T, email, task, deadlineDate, metadata string, done, isDeleted int) MessageID {
	t.Helper()
	src := testutil.RandomTS("pe")
	res, err := GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  requester, assignee, deadline_date, created_at, updated_at)
		 VALUES (?, ?, 'TASK', 'slack', 'general', ?, ?, ?, ?, ?, 'bob', ?, datetime('now'), datetime('now'))`,
		email, task, src, done, isDeleted, metadata, email, deadlineDate,
	)
	if err != nil {
		t.Fatalf("seedPastEventTask: %v", err)
	}
	id, _ := res.LastInsertId()
	return MessageID(id)
}

func TestListPastEventCandidates(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("pastevent")
	cutoff := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	lookbackFloor := cutoff.AddDate(0, 0, -29)

	inRange := seedPastEventTask(t, email, "Join Mesiniaga session", "2026-09-25", "{}", 0, 0)
	seedPastEventTask(t, email, "Too recent", "2026-09-26", "{}", 0, 0)         // == cutoff, excluded
	seedPastEventTask(t, email, "Too old", "2026-08-20", "{}", 0, 0)            // before lookback floor
	seedPastEventTask(t, email, "Already candidate", "2026-09-20",
		`{"completion_candidate":{"status":"pending"}}`, 0, 0)
	invalidJSON := seedPastEventTask(t, email, "Malformed metadata", "2026-09-21", "{not json", 0, 0)
	seedPastEventTask(t, email, "Done task", "2026-09-22", "{}", 1, 0)     // done, excluded
	seedPastEventTask(t, email, "Deleted task", "2026-09-23", "{}", 0, 1) // canceled, excluded

	rows, err := ListPastEventCandidates(ctx, email, cutoff, lookbackFloor, 97)
	if err != nil {
		t.Fatalf("ListPastEventCandidates: %v", err)
	}
	got := make(map[MessageID]bool)
	for _, r := range rows {
		got[r.ID] = true
	}
	if !got[inRange] {
		t.Errorf("expected in-range candidate %d in results", inRange)
	}
	if !got[invalidJSON] {
		t.Errorf("expected malformed-metadata row %d to be treated as no candidate", invalidJSON)
	}
	if len(rows) != 2 {
		t.Errorf("expected exactly 2 candidates, got %d: %+v", len(rows), rows)
	}
}
