package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
)

// seedExclusionTask inserts an open TASK row whose last activity was daysStalled days ago.
func seedExclusionTask(t *testing.T, email, task string, daysStalled int, metadata string) MessageID {
	t.Helper()
	src := testutil.RandomTS("exc")
	createdAt := time.Now().UTC().AddDate(0, 0, -daysStalled).Format(time.RFC3339)
	res, err := GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  requester, assignee, created_at, updated_at)
		 VALUES (?, ?, 'TASK', 'slack', 'general', ?, 0, 0, ?, ?, 'bob', ?, '1970-01-01T00:00:00Z')`,
		email, task, src, metadata, email, createdAt,
	)
	if err != nil {
		t.Fatalf("seedExclusionTask: %v", err)
	}
	id, _ := res.LastInsertId()
	return MessageID(id)
}

func exclusionRowState(t *testing.T, id MessageID) (lifecycle string, excludedAt sql.NullString, metadata string) {
	t.Helper()
	err := GetDB().QueryRow(
		`SELECT lifecycle, excluded_at, COALESCE(metadata,'{}') FROM messages WHERE id = ?`, int64(id),
	).Scan(&lifecycle, &excludedAt, &metadata)
	if err != nil {
		t.Fatalf("exclusionRowState: %v", err)
	}
	return lifecycle, excludedAt, metadata
}

func TestSelectExclusionScan_Threshold(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("scan")
	oldID := seedExclusionTask(t, email, "Very old task", 35, "{}")
	seedExclusionTask(t, email, "Recent task", 10, "{}")

	rows, err := SelectExclusionScan(context.Background(), 31)
	if err != nil {
		t.Fatalf("SelectExclusionScan: %v", err)
	}
	var found int
	for _, r := range rows {
		if r.UserEmail != email {
			continue
		}
		found++
		if r.ID != oldID {
			t.Errorf("expected only old task %d, got %d", oldID, r.ID)
		}
		if r.DaysStalled < 31 {
			t.Errorf("expected days_stalled >= 31, got %d", r.DaysStalled)
		}
	}
	if found != 1 {
		t.Errorf("expected 1 scan row for %s, got %d", email, found)
	}
}

func TestExclusionCandidateRoundTrip(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("round")
	id := seedExclusionTask(t, email, "Round trip task", 35, "{}")

	cand := ExclusionCandidate{ProposedAt: time.Now().UTC().Format(time.RFC3339), DaysStalled: 35, Status: "pending"}
	if err := ProposeExclusionCandidate(ctx, GetDB(), email, id, cand); err != nil {
		t.Fatalf("propose: %v", err)
	}
	_, _, meta := exclusionRowState(t, id)
	if !HasExclusionCandidate(meta) {
		t.Fatalf("expected exclusion candidate in metadata, got %s", meta)
	}

	if err := ConfirmExclusion(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	lifecycle, excludedAt, meta := exclusionRowState(t, id)
	if lifecycle != "excluded" {
		t.Errorf("expected lifecycle=excluded, got %s", lifecycle)
	}
	if !excludedAt.Valid {
		t.Errorf("expected excluded_at set")
	}
	var confirmed ExclusionCandidate
	if ParseMetadata(meta).Decode(metaKeyExclusionCandidate, &confirmed) {
		if confirmed.Status != "confirmed" {
			t.Errorf("expected candidate status confirmed, got %v", confirmed.Status)
		}
	} else {
		t.Errorf("expected candidate kept with confirmed status")
	}

	// Excluded rows must drop out of stalled detection.
	buckets, err := SelectStalled(ctx, email, 3)
	if err != nil {
		t.Fatalf("SelectStalled: %v", err)
	}
	if len(buckets.Mine)+len(buckets.Observed) != 0 {
		t.Errorf("excluded task must not appear in stalled buckets")
	}

	// And out of the candidate scan.
	rows, err := SelectExclusionScan(ctx, 31)
	if err != nil {
		t.Fatalf("scan after confirm: %v", err)
	}
	for _, r := range rows {
		if r.ID == id {
			t.Errorf("excluded task must not appear in exclusion scan")
		}
	}

	if err := RestoreExcluded(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("restore: %v", err)
	}
	lifecycle, excludedAt, meta = exclusionRowState(t, id)
	if lifecycle != "active" {
		t.Errorf("expected lifecycle=active after restore, got %s", lifecycle)
	}
	if excludedAt.Valid {
		t.Errorf("expected excluded_at cleared after restore")
	}
	if HasExclusionCandidate(meta) {
		t.Errorf("expected exclusion markers cleared after restore, got %s", meta)
	}

	// Second restore is a no-op on a non-excluded row.
	if err := RestoreExcluded(ctx, GetDB(), email, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows on double restore, got %v", err)
	}
}

func TestDismissExclusionCandidate(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("dismiss")
	id := seedExclusionTask(t, email, "Dismiss me", 35, "{}")

	cand := ExclusionCandidate{ProposedAt: time.Now().UTC().Format(time.RFC3339), DaysStalled: 35, Status: "pending"}
	if err := ProposeExclusionCandidate(ctx, GetDB(), email, id, cand); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := DismissExclusionCandidate(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	lifecycle, _, meta := exclusionRowState(t, id)
	if lifecycle != "active" {
		t.Errorf("dismiss must keep the task active, got %s", lifecycle)
	}
	if HasExclusionCandidate(meta) {
		t.Errorf("expected candidate removed after dismiss")
	}
	if _, ok := ExclusionDismissedAt(meta); !ok {
		t.Errorf("expected dismissed_at stamped, got %s", meta)
	}
}

func TestConfirmExclusion_TerminalStateGuard(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("guard")
	id := seedExclusionTask(t, email, "Done task", 35, "{}")
	if _, err := GetDB().Exec(`UPDATE messages SET done = 1 WHERE id = ?`, int64(id)); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	if err := ConfirmExclusion(ctx, GetDB(), email, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows for done task, got %v", err)
	}
}

func TestLifecycle_DoneOutranksExcluded(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("rank")
	id := seedExclusionTask(t, email, "Rank task", 35, "{}")
	if err := ConfirmExclusion(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if _, err := GetDB().Exec(`UPDATE messages SET done = 1 WHERE id = ?`, int64(id)); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	lifecycle, _, _ := exclusionRowState(t, id)
	if lifecycle != "done" {
		t.Errorf("done must outrank excluded, got %s", lifecycle)
	}
}

func TestRestoreMessages_ClearsExclusionState(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("bulk")
	id := seedExclusionTask(t, email, "Bulk restore task", 35, "{}")
	cand := ExclusionCandidate{ProposedAt: time.Now().UTC().Format(time.RFC3339), DaysStalled: 35, Status: "pending"}
	if err := ProposeExclusionCandidate(ctx, GetDB(), email, id, cand); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := ConfirmExclusion(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	if err := db.New(GetDB()).RestoreMessages(ctx, db.RestoreMessagesParams{
		UserEmail: nullString(email),
		Ids:       []int64{int64(id)},
	}); err != nil {
		t.Fatalf("RestoreMessages: %v", err)
	}
	lifecycle, excludedAt, meta := exclusionRowState(t, id)
	if lifecycle != "active" {
		t.Errorf("expected active after bulk restore, got %s", lifecycle)
	}
	if excludedAt.Valid {
		t.Errorf("expected excluded_at cleared by bulk restore")
	}
	if HasExclusionCandidate(meta) {
		t.Errorf("expected exclusion markers cleared by bulk restore, got %s", meta)
	}
}

func TestAutoRestoreIfExcluded(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("auto")
	id := seedExclusionTask(t, email, "Auto restore task", 35, "{}")
	if err := ConfirmExclusion(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	restored, err := AutoRestoreIfExcluded(ctx, GetDB(), email, id)
	if err != nil || !restored {
		t.Fatalf("expected restore, got restored=%v err=%v", restored, err)
	}
	lifecycle, _, meta := exclusionRowState(t, id)
	if lifecycle != "active" {
		t.Errorf("expected active after auto-restore, got %s", lifecycle)
	}
	if !ParseMetadata(meta).Has(metaKeyExcludedAutoRestoredAt) {
		t.Errorf("expected excluded_auto_restored_at stamped, got %s", meta)
	}

	restored, err = AutoRestoreIfExcluded(ctx, GetDB(), email, id)
	if err != nil || restored {
		t.Errorf("expected no-op on non-excluded row, got restored=%v err=%v", restored, err)
	}
}

func TestArchiveStatusExcluded(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("arch")
	id := seedExclusionTask(t, email, "Archived excluded task", 35, "{}")
	seedExclusionTask(t, email, "Still active task", 35, "{}")
	if err := ConfirmExclusion(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	total, err := db.New(GetDB()).SearchArchivedMessagesCount(ctx, db.SearchArchivedMessagesCountParams{
		UserEmail: nullString(email),
		Column2:   "",
		Column3:   "excluded",
	})
	if err != nil {
		t.Fatalf("archive count: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 excluded archive row, got %d", total)
	}

	rows, err := db.New(GetDB()).SearchArchivedMessages(ctx, db.SearchArchivedMessagesParams{
		UserEmail: nullString(email),
		Column2:   "",
		Column3:   "excluded",
		Limit:     10,
		Offset:    0,
	})
	if err != nil {
		t.Fatalf("archive search: %v", err)
	}
	if len(rows) != 1 || MessageID(rows[0].ID) != id {
		t.Fatalf("expected the excluded row only, got %d rows", len(rows))
	}
	msgs := mapRowSliceToMessage(rows)
	if msgs[0].ExcludedAt == nil {
		t.Errorf("expected ExcludedAt populated on archive mapping")
	}
	if !statusMatch(msgs[0], "excluded") {
		t.Errorf("expected statusMatch(excluded) to be true")
	}
}

func TestSelectExcludedDigestItems(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("digest")
	id := seedExclusionTask(t, email, "Parked task", 40, "{}")
	if err := ConfirmExclusion(ctx, GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	items, err := SelectExcludedDigestItems(ctx)
	if err != nil {
		t.Fatalf("digest items: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.ID == id {
			found = true
			if it.ExcludedAt.IsZero() {
				t.Errorf("expected ExcludedAt populated")
			}
		}
	}
	if !found {
		t.Errorf("expected parked task in digest items")
	}
}

// TestMigrateLifecycleExcluded_Legacy verifies the v15 DROP/re-ADD of the lifecycle
// generated column on a legacy-shaped messages table, plus idempotency on re-run.
func TestMigrateLifecycleExcluded_Legacy(t *testing.T) {
	raw, err := sql.Open("sqlite", fmt.Sprintf("file:legacy_%d?mode=memory", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	const legacyDDL = `CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_email TEXT, task TEXT,
		done BOOLEAN DEFAULT 0, is_deleted BOOLEAN DEFAULT 0,
		category TEXT DEFAULT 'todo', metadata TEXT DEFAULT '{}',
		lifecycle TEXT GENERATED ALWAYS AS (
			CASE
				WHEN category = 'merged'         THEN 'merged'
				WHEN done = 0 AND is_deleted = 1 THEN 'canceled'
				WHEN done = 1 AND is_deleted = 1 THEN 'swept'
				WHEN done = 1                    THEN 'done'
				ELSE 'active'
			END
		) VIRTUAL
	)`
	if _, err := raw.Exec(legacyDDL); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	seed := []struct {
		task      string
		done      int
		isDeleted int
		category  string
		want      string
	}{
		{"active one", 0, 0, "TASK", "active"},
		{"done one", 1, 0, "TASK", "done"},
		{"canceled one", 0, 1, "TASK", "canceled"},
		{"merged one", 0, 0, "merged", "merged"},
	}
	for _, s := range seed {
		if _, err := raw.Exec(
			`INSERT INTO messages (user_email, task, done, is_deleted, category) VALUES ('u@test.com', ?, ?, ?, ?)`,
			s.task, s.done, s.isDeleted, s.category,
		); err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}
	}

	ctx := context.Background()
	if err := migrateLifecycleExcluded(ctx, raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Existing rows keep their lifecycle labels.
	for _, s := range seed {
		var got string
		if err := raw.QueryRow(`SELECT lifecycle FROM messages WHERE task = ?`, s.task).Scan(&got); err != nil {
			t.Fatalf("read lifecycle for %s: %v", s.task, err)
		}
		if got != s.want {
			t.Errorf("task %q: expected lifecycle %s, got %s", s.task, s.want, got)
		}
	}

	// New branch works.
	if _, err := raw.Exec(`UPDATE messages SET excluded_at = CURRENT_TIMESTAMP WHERE task = 'active one'`); err != nil {
		t.Fatalf("set excluded_at: %v", err)
	}
	var got string
	if err := raw.QueryRow(`SELECT lifecycle FROM messages WHERE task = 'active one'`).Scan(&got); err != nil {
		t.Fatalf("read excluded lifecycle: %v", err)
	}
	if got != "excluded" {
		t.Errorf("expected lifecycle=excluded, got %s", got)
	}

	// Idempotent: second run must be a clean no-op.
	if err := migrateLifecycleExcluded(ctx, raw); err != nil {
		t.Errorf("re-run must be idempotent, got %v", err)
	}
}
