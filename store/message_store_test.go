package store

import (
	"context"
	"database/sql"
	"message-consolidator/internal/testutil"
	"strings"
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

// TestSaveMessage_SemanticDup verifies that when SaveMessage detects a semantic duplicate
// it does NOT insert a new row, appends the new text to the existing task's original_text,
// and returns (saved=false, matchedID, nil).
func TestSaveMessage_SemanticDup(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("semdup")
	room := "biz-test"

	// Insert existing open task that will be the dup target.
	existingTask := "Resolve Carabao issue by updating K8s agent to latest version"
	existingOriginal := "original reply text"
	existing := ConsolidatedMessage{
		UserEmail:    email,
		Source:       "slack",
		Room:         room,
		Task:         existingTask,
		OriginalText: existingOriginal,
		SourceTS:     testutil.RandomTS("existing"),
	}
	saved, existingID, err := SaveMessage(ctx, GetDB(), existing)
	if err != nil || !saved || existingID == 0 {
		t.Fatalf("seed SaveMessage: saved=%v id=%d err=%v", saved, existingID, err)
	}

	// Invalidate cache so GetActiveContextTasks picks up the newly inserted row.
	InvalidateCacheActive(email)

	// New candidate with same task title (sim == 1.0 ≥ 0.85) but different source_ts.
	newText := "agent bug, update agent latest or try preview"
	candidate := ConsolidatedMessage{
		UserEmail:    email,
		Source:       "slack",
		Room:         room,
		Task:         existingTask,
		OriginalText: newText,
		SourceTS:     testutil.RandomTS("dup"),
	}
	dupSaved, dupID, dupErr := SaveMessage(ctx, GetDB(), candidate)

	if dupErr != nil {
		t.Fatalf("SaveMessage on dup: unexpected error %v", dupErr)
	}
	if dupSaved {
		t.Error("SaveMessage on dup: expected saved=false, got true")
	}
	if dupID != existingID {
		t.Errorf("SaveMessage on dup: expected matchedID=%d, got %d", existingID, dupID)
	}

	// Verify original_text was prepended: NEW || '\n\n' || OLD.
	var gotOriginal string
	row := GetDB().QueryRowContext(ctx, `SELECT original_text FROM messages WHERE id = ?`, int64(existingID))
	if err := row.Scan(&gotOriginal); err != nil {
		t.Fatalf("read original_text: %v", err)
	}
	wantPrefix := newText + "\n\n" + existingOriginal
	if gotOriginal != wantPrefix {
		t.Errorf("original_text mismatch:\n  got  %q\n  want %q", gotOriginal, wantPrefix)
	}

	// Verify no new row was inserted (still only 1 message row for this email).
	var count int
	if err := GetDB().QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE user_email = ?`, email).Scan(&count); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 message row after semantic dup, got %d", count)
	}
}

// TestSaveMessage_CrossThreadGuard verifies that semantic-dup detection respects thread boundaries.
func TestSaveMessage_CrossThreadGuard(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	similarTask := "Resolve Carabao issue by updating K8s agent to latest version"
	dissimilarTask := "Completely unrelated grocery shopping reminder"

	cases := []struct {
		name           string
		existingThread string
		newThread      string
		newTask        string
		wantDup        bool
	}{
		{"same thread similar task", "T1", "T1", similarTask, true},
		{"different thread similar task", "T1", "T2", similarTask, false},
		{"both empty thread top-level", "", "", similarTask, true},
		{"different thread dissimilar task", "T1", "T2", dissimilarTask, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			email := testutil.RandomEmail("xthread")
			room := "biz-test-cross"

			existing := ConsolidatedMessage{
				UserEmail:    email,
				Source:       "slack",
				Room:         room,
				Task:         similarTask,
				OriginalText: "original text",
				SourceTS:     testutil.RandomTS("existing"),
				ThreadID:     tc.existingThread,
			}
			_, existingID, err := SaveMessage(ctx, GetDB(), existing)
			if err != nil || existingID == 0 {
				t.Fatalf("seed: %v", err)
			}
			InvalidateCacheActive(email)

			candidate := ConsolidatedMessage{
				UserEmail:    email,
				Source:       "slack",
				Room:         room,
				Task:         tc.newTask,
				OriginalText: "new thread text",
				SourceTS:     testutil.RandomTS("new"),
				ThreadID:     tc.newThread,
			}
			saved, matchID, err := SaveMessage(ctx, GetDB(), candidate)
			if err != nil {
				t.Fatalf("SaveMessage: %v", err)
			}

			if tc.wantDup {
				if saved {
					t.Errorf("%s: expected saved=false (dup), got true", tc.name)
				}
				if matchID != existingID {
					t.Errorf("%s: expected matchID=%d, got %d", tc.name, existingID, matchID)
				}
			} else {
				if !saved {
					t.Errorf("%s: expected saved=true (new row), got false (incorrectly merged)", tc.name)
				}
			}
		})
	}
}

// readMetadata fetches the raw metadata column for assertions on completion-candidate state.
func readMetadata(ctx context.Context, t *testing.T, id MessageID) string {
	t.Helper()
	var meta sql.NullString
	row := GetDB().QueryRowContext(ctx, `SELECT metadata FROM messages WHERE id = ?`, int64(id))
	if err := row.Scan(&meta); err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	return meta.String
}

// TestMarkMessageDone_ClearsCompletionCandidate verifies both the done=true and
// done=false (unmark) raw-SQL paths clear a pending completion_candidate so a
// task explicitly closed/reopened by the user never resurfaces a stale suggestion.
func TestMarkMessageDone_ClearsCompletionCandidate(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("markdone")
	msg := ConsolidatedMessage{UserEmail: email, Source: "slack", Room: "biz-md", Task: "t", SourceTS: testutil.RandomTS("md")}
	_, id, err := SaveMessage(ctx, GetDB(), msg)
	if err != nil || id == 0 {
		t.Fatalf("seed SaveMessage: id=%d err=%v", id, err)
	}

	cand := CompletionCandidate{SourceLink: "link1", Status: "pending", DetectedAt: "2024-01-01T00:00:00Z"}
	if err := AddCompletionCandidate(ctx, GetDB(), email, id, cand); err != nil {
		t.Fatalf("AddCompletionCandidate: %v", err)
	}

	if err := MarkMessageDone(ctx, GetDB(), email, id, true); err != nil {
		t.Fatalf("MarkMessageDone(true): %v", err)
	}
	if meta := readMetadata(ctx, t, id); strings.Contains(meta, "completion_candidate") {
		t.Errorf("expected completion_candidate cleared after MarkMessageDone(true), got %q", meta)
	}

	if err := AddCompletionCandidate(ctx, GetDB(), email, id, cand); err != nil {
		t.Fatalf("AddCompletionCandidate (re-add): %v", err)
	}
	if err := MarkMessageDone(ctx, GetDB(), email, id, false); err != nil {
		t.Fatalf("MarkMessageDone(false): %v", err)
	}
	if meta := readMetadata(ctx, t, id); strings.Contains(meta, "completion_candidate") {
		t.Errorf("expected completion_candidate cleared after unmark, got %q", meta)
	}
}

// TestDismissCompletionCandidate_RecordsMarkerAndClears verifies dismissal clears
// the pending candidate and records a source-scoped marker that WasCandidateDismissed
// can later match, preventing the exact same source from re-suggesting completion.
func TestDismissCompletionCandidate_RecordsMarkerAndClears(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("dismiss")
	msg := ConsolidatedMessage{UserEmail: email, Source: "slack", Room: "biz-ds", Task: "t", SourceTS: testutil.RandomTS("ds")}
	_, id, err := SaveMessage(ctx, GetDB(), msg)
	if err != nil || id == 0 {
		t.Fatalf("seed SaveMessage: id=%d err=%v", id, err)
	}

	cand := CompletionCandidate{SourceLink: "https://slack.com/archives/C/p1", Status: "pending", DetectedAt: "2024-01-01T00:00:00Z"}
	if err := AddCompletionCandidate(ctx, GetDB(), email, id, cand); err != nil {
		t.Fatalf("AddCompletionCandidate: %v", err)
	}

	if err := DismissCompletionCandidate(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("DismissCompletionCandidate: %v", err)
	}

	meta := readMetadata(ctx, t, id)
	if strings.Contains(meta, `"completion_candidate"`) {
		t.Errorf("expected completion_candidate cleared after dismiss, got %q", meta)
	}
	if !WasCandidateDismissed(meta, cand.SourceLink) {
		t.Errorf("expected WasCandidateDismissed=true for the dismissed source_link, metadata=%q", meta)
	}
	if WasCandidateDismissed(meta, "https://slack.com/archives/other") {
		t.Error("expected WasCandidateDismissed=false for a different source link")
	}
}
