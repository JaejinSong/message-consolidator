package store

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
)

func candidateMeta(detectedAt, text string) string {
	return `{"completion_candidate":{"source_text":"` + text + `","detected_at":"` + detectedAt + `","status":"pending"}}`
}

func TestListPendingCompletionCandidates(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()
	ctx := context.Background()

	email := testutil.RandomEmail("cand")
	newer := seedExclusionTask(t, email, "newer", 1, candidateMeta("2026-10-02T00:00:00Z", "done B"))
	older := seedExclusionTask(t, email, "older", 1, candidateMeta("2026-10-01T00:00:00Z", "done A"))
	doneID := seedExclusionTask(t, email, "finished", 1, candidateMeta("2026-10-01T00:00:00Z", "x"))
	dismissed := seedExclusionTask(t, email, "dismissed", 1, candidateMeta("2026-10-01T00:00:00Z", "y"))
	seedExclusionTask(t, email, "no candidate", 1, "{}")
	seedExclusionTask(t, testutil.RandomEmail("other"), "other user", 1, candidateMeta("2026-10-01T00:00:00Z", "z"))

	if _, err := GetDB().Exec(`UPDATE messages SET done = 1 WHERE id = ?`, int64(doneID)); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if err := DismissCompletionCandidate(ctx, GetDB(), email, dismissed); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	rows, err := ListPendingCompletionCandidates(ctx, email, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != older || rows[1].ID != newer {
		t.Fatalf("rows = %+v, want [older newer]", rows)
	}
	if rows[0].SourceText != "done A" || rows[0].DetectedAt != "2026-10-01T00:00:00Z" || rows[0].Source != "slack" {
		t.Errorf("row fields = %+v", rows[0])
	}

	limited, err := ListPendingCompletionCandidates(ctx, email, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit: rows=%d err=%v", len(limited), err)
	}
}
