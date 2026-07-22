package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func exclusionMeta(t *testing.T, id store.MessageID) string {
	t.Helper()
	var meta string
	if err := store.GetDB().QueryRow(
		`SELECT COALESCE(metadata,'{}') FROM messages WHERE id = ?`, int64(id),
	).Scan(&meta); err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	return meta
}

// seedAgedTask inserts an open TASK row whose fallback activity timestamp is daysAgo old.
func seedAgedTask(t *testing.T, email, task, metadata string, daysAgo int) store.MessageID {
	t.Helper()
	src := testutil.RandomTS("svc")
	createdAt := time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	res, err := store.GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  requester, assignee, created_at, updated_at)
		 VALUES (?, ?, 'TASK', 'slack', 'general', ?, 0, 0, ?, ?, 'bob', ?, '1970-01-01T00:00:00Z')`,
		email, task, src, metadata, email, createdAt,
	)
	if err != nil {
		t.Fatalf("seedAgedTask: %v", err)
	}
	id, _ := res.LastInsertId()
	return store.MessageID(id)
}

func TestShouldProposeExclusion(t *testing.T) {
	now := time.Now().UTC()
	mustJSON := func(m map[string]any) string {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	tests := []struct {
		name      string
		metadata  string
		updatedAt time.Time
		want      bool
	}{
		{"empty metadata", "{}", time.Time{}, true},
		{"existing candidate", mustJSON(map[string]any{
			"exclusion_candidate": map[string]any{"status": "pending"},
		}), time.Time{}, false},
		{"pending completion candidate", mustJSON(map[string]any{
			"completion_candidate": map[string]any{"status": "pending"},
		}), time.Time{}, false},
		{"dismissed recently, no new activity", mustJSON(map[string]any{
			"exclusion_candidate_dismissed_at": now.AddDate(0, 0, -10).Format(time.RFC3339),
		}), time.Time{}, false},
		{"dismissed past repropose window", mustJSON(map[string]any{
			"exclusion_candidate_dismissed_at": now.AddDate(0, 0, -62).Format(time.RFC3339),
		}), time.Time{}, true},
		{"dismissed but new activity after dismissal", mustJSON(map[string]any{
			"exclusion_candidate_dismissed_at": now.AddDate(0, 0, -10).Format(time.RFC3339),
		}), now.AddDate(0, 0, -5), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := store.ExclusionScanRow{Metadata: tc.metadata, UpdatedAt: tc.updatedAt}
			if got := shouldProposeExclusion(row, now); got != tc.want {
				t.Errorf("shouldProposeExclusion = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProposeExclusionCandidates_EndToEnd(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("propose")
	now := time.Now().UTC()

	eligible := seedAgedTask(t, email, "Old idle task", "{}", 35)
	recent := seedAgedTask(t, email, "Fresh task", "{}", 5)
	dismissedRecently := seedAgedTask(t, email, "Recently dismissed", `{"exclusion_candidate_dismissed_at":"`+now.AddDate(0, 0, -10).Format(time.RFC3339)+`"}`, 35)
	dismissedLongAgo := seedAgedTask(t, email, "Dismissed long ago", `{"exclusion_candidate_dismissed_at":"`+now.AddDate(0, 0, -62).Format(time.RFC3339)+`"}`, 70)
	completionPending := seedAgedTask(t, email, "Completion pending", `{"completion_candidate":{"status":"pending"}}`, 35)

	svc := NewExclusionService(nil)
	if err := svc.ProposeExclusionCandidates(ctx); err != nil {
		t.Fatalf("propose candidates: %v", err)
	}

	expect := map[store.MessageID]bool{
		eligible:          true,
		recent:            false,
		dismissedRecently: false,
		dismissedLongAgo:  true,
		completionPending: false,
	}
	for id, want := range expect {
		got := store.HasExclusionCandidate(exclusionMeta(t, id))
		if got != want {
			t.Errorf("task %d: candidate=%v, want %v", id, got, want)
		}
	}
}

func TestDispatchExcludedDigest(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("exdigest")
	seedUserWithSlack(t, email, "UEXDIGEST")

	id := seedAgedTask(t, email, "Parked forever", "{}", 90)
	if err := store.ConfirmExclusion(ctx, store.GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	// Backdate excluded_at past the 29-day digest interval.
	oldTS := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	if _, err := store.GetDB().Exec(`UPDATE messages SET excluded_at = ? WHERE id = ?`, oldTS, int64(id)); err != nil {
		t.Fatalf("backdate excluded_at: %v", err)
	}

	slack := &fakeSlack{}
	svc := NewExclusionService(slack)
	if err := svc.DispatchExcludedDigest(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(slack.sent) != 1 {
		t.Fatalf("expected 1 digest DM, got %d", len(slack.sent))
	}
	if !strings.Contains(slack.sent[0], "Parked forever") {
		t.Errorf("digest must list the parked task, got %q", slack.sent[0])
	}

	// Idempotent within the interval: second dispatch sends nothing.
	if err := svc.DispatchExcludedDigest(ctx); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if len(slack.sent) != 1 {
		t.Errorf("expected no re-send within interval, got %d DMs", len(slack.sent))
	}
}

func TestDispatchExcludedDigest_RecentExclusionSkipped(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("exrecent")
	seedUserWithSlack(t, email, "UEXRECENT")

	id := seedAgedTask(t, email, "Freshly parked", "{}", 40)
	if err := store.ConfirmExclusion(ctx, store.GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	slack := &fakeSlack{}
	svc := NewExclusionService(slack)
	if err := svc.DispatchExcludedDigest(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(slack.sent) != 0 {
		t.Errorf("expected no DM for exclusion younger than the interval, got %d", len(slack.sent))
	}
}

func TestDispatchExcludedDigest_SendFailureNotMarked(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("exfail")
	seedUserWithSlack(t, email, "UEXFAIL")

	id := seedAgedTask(t, email, "Retry me", "{}", 90)
	if err := store.ConfirmExclusion(ctx, store.GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	oldTS := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	if _, err := store.GetDB().Exec(`UPDATE messages SET excluded_at = ? WHERE id = ?`, oldTS, int64(id)); err != nil {
		t.Fatalf("backdate excluded_at: %v", err)
	}

	failing := &fakeSlack{err: errors.New("slack down")}
	svc := NewExclusionService(failing)
	if err := svc.DispatchExcludedDigest(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if store.RemindedWithin(exclusionMeta(t, id), "excluded_digest", 29*24*time.Hour) {
		t.Errorf("failed send must not be marked as reminded")
	}

	// Retry with a working sender succeeds and marks.
	working := &fakeSlack{}
	svc = NewExclusionService(working)
	if err := svc.DispatchExcludedDigest(ctx); err != nil {
		t.Fatalf("retry dispatch: %v", err)
	}
	if len(working.sent) != 1 {
		t.Errorf("expected retry to send, got %d", len(working.sent))
	}
	if !store.RemindedWithin(exclusionMeta(t, id), "excluded_digest", 29*24*time.Hour) {
		t.Errorf("successful send must mark reminded_at_excluded_digest")
	}
}

func TestDispatchExcludedDigest_Cap(t *testing.T) {
	cleanup := setupReminderTestDB(t)
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("excap")
	seedUserWithSlack(t, email, "UEXCAP")

	oldTS := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	for i := 0; i < excludedDigestCap+2; i++ {
		id := seedAgedTask(t, email, "Capped task", "{}", 90)
		if err := store.ConfirmExclusion(ctx, store.GetDB(), email, id); err != nil {
			t.Fatalf("confirm: %v", err)
		}
		if _, err := store.GetDB().Exec(`UPDATE messages SET excluded_at = ? WHERE id = ?`, oldTS, int64(id)); err != nil {
			t.Fatalf("backdate excluded_at: %v", err)
		}
	}

	slack := &fakeSlack{}
	svc := NewExclusionService(slack)
	if err := svc.DispatchExcludedDigest(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(slack.sent) != 1 {
		t.Fatalf("expected a single capped digest DM, got %d", len(slack.sent))
	}
	bullets := strings.Count(slack.sent[0], "•")
	if bullets != excludedDigestCap {
		t.Errorf("expected %d bullet lines, got %d", excludedDigestCap, bullets)
	}
}
