package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
)

// lifecycleFixture describes the seven lifecycle states exercised against the queries that
// were auditing done=0/is_deleted=0 as a stand-in for "open task" (they also matched merged
// and excluded rows). Field values mirror the CASE precedence in migrations.go so the
// generated `lifecycle` column lands on the intended state for each row.
type lifecycleFixture struct {
	state       string
	category    string
	done        int
	isDeleted   int
	hasDeadline bool
	completed   bool
	excluded    bool
}

var lifecycleFixtures = []lifecycleFixture{
	{state: "active", category: "PROMISE", hasDeadline: true},
	{state: "done", category: "PROMISE", done: 1, completed: true},
	{state: "swept", category: "PROMISE", done: 1, isDeleted: 1},
	{state: "canceled", category: "PROMISE", isDeleted: 1},
	{state: "merged", category: "merged", hasDeadline: true},
	{state: "excluded", category: "PROMISE", excluded: true},
	{state: "merged_excluded", category: "merged", excluded: true},
}

// seedLifecycleRow inserts one row per lifecycleFixture, all sharing thread_id/room/source
// so thread- and context-scoped queries see the full set.
func seedLifecycleRow(t *testing.T, email, room, threadID string, f lifecycleFixture) MessageID {
	t.Helper()
	src := testutil.RandomTS("lc-" + f.state)
	now := time.Now().UTC()

	var deadline any
	if f.hasDeadline {
		deadline = now.Add(48 * time.Hour).Format(time.RFC3339)
	}
	var completedAt any
	if f.completed {
		completedAt = now.AddDate(0, 0, -1).Format(time.RFC3339)
	}
	var excludedAt any
	if f.excluded {
		excludedAt = now.Format(time.RFC3339)
	}

	res, err := GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, thread_id, source_ts, done, is_deleted,
		  deadline, completed_at, excluded_at, requester, assignee, metadata,
		  created_at, updated_at)
		 VALUES (?, ?, ?, 'gmail', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)`,
		email, "Task "+f.state, f.category, room, threadID, src, f.done, f.isDeleted,
		deadline, completedAt, excludedAt, email, "bob", now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("seedLifecycleRow(%s): %v", f.state, err)
	}
	id, _ := res.LastInsertId()
	return MessageID(id)
}

func seedAllLifecycleStates(t *testing.T, email, room, threadID string) map[string]MessageID {
	t.Helper()
	ids := make(map[string]MessageID, len(lifecycleFixtures))
	for _, f := range lifecycleFixtures {
		ids[f.state] = seedLifecycleRow(t, email, room, threadID, f)
	}
	return ids
}

func idSet(msgs []ConsolidatedMessage) map[MessageID]bool {
	out := make(map[MessageID]bool, len(msgs))
	for _, m := range msgs {
		out[m.ID] = true
	}
	return out
}

func assertIDSet(t *testing.T, label string, got map[MessageID]bool, ids map[string]MessageID, wantStates ...string) {
	t.Helper()
	want := make(map[MessageID]bool, len(wantStates))
	for _, s := range wantStates {
		want[ids[s]] = true
	}
	if len(got) != len(want) {
		t.Errorf("%s: expected %d rows (%v), got %d", label, len(want), wantStates, len(got))
	}
	for id := range want {
		if !got[id] {
			t.Errorf("%s: expected row %d present, missing", label, id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("%s: unexpected row %d present", label, id)
		}
	}
}

func TestLifecycleQueries_ExcludeMergedAndTerminalStates(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("lifecycle")
	room := "general"
	threadID := "thread-lifecycle"
	ids := seedAllLifecycleStates(t, email, room, threadID)

	incomplete, err := GetIncompleteByThreadID(ctx, GetDB(), email, threadID)
	if err != nil {
		t.Fatalf("GetIncompleteByThreadID: %v", err)
	}
	assertIDSet(t, "GetIncompleteByThreadID", idSet(incomplete), ids, "active", "excluded")

	contextTasks, err := GetActiveContextTasks(ctx, GetDB(), email, "gmail", room)
	if err != nil {
		t.Fatalf("GetActiveContextTasks: %v", err)
	}
	assertIDSet(t, "GetActiveTasksForContext", idSet(contextTasks), ids, "active", "excluded", "done")

	gmailIncomplete, err := GetRecentIncompleteGmail(ctx, GetDB(), email)
	if err != nil {
		t.Fatalf("GetRecentIncompleteGmail: %v", err)
	}
	assertIDSet(t, "GetRecentIncompleteGmail", idSet(gmailIncomplete), ids, "active", "excluded")

	now := time.Now().UTC()
	dueSoon, err := SelectDueSoon(ctx, now.Format(time.RFC3339), now.Add(72*time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatalf("SelectDueSoon: %v", err)
	}
	dueSoonGot := make(map[MessageID]bool)
	for _, r := range dueSoon {
		if r.UserEmail == email {
			dueSoonGot[r.ID] = true
		}
	}
	assertIDSet(t, "SelectDueSoonMessages", dueSoonGot, ids, "active")

	rows, err := db.New(GetDB()).SelectCommitments(ctx, db.SelectCommitmentsParams{
		UserEmail:          email,
		AssigneeCanonical:  "bob",
		RequesterCanonical: "bob",
	})
	if err != nil {
		t.Fatalf("SelectCommitments: %v", err)
	}
	commitmentsGot := make(map[MessageID]bool)
	for _, r := range rows {
		commitmentsGot[MessageID(r.ID)] = true
	}
	assertIDSet(t, "SelectCommitments", commitmentsGot, ids, "active")

	// Why: SelectUndatedCommitments requires no deadline; clear it on the active row only
	// after the due-soon assertion above already exercised its deadline-bearing form.
	if _, err := GetDB().Exec(
		`UPDATE messages SET deadline = NULL, deadline_date = NULL WHERE id = ?`,
		int64(ids["active"]),
	); err != nil {
		t.Fatalf("clear active deadline: %v", err)
	}
	undated, err := SelectUndated(ctx)
	if err != nil {
		t.Fatalf("SelectUndated: %v", err)
	}
	undatedGot := make(map[MessageID]bool)
	for _, r := range undated {
		if r.UserEmail == email {
			undatedGot[r.ID] = true
		}
	}
	assertIDSet(t, "SelectUndatedCommitments", undatedGot, ids, "active")

	// ConfirmExclusion must only ever affect the active row.
	if err := ConfirmExclusion(ctx, GetDB(), email, ids["active"]); err != nil {
		t.Fatalf("ConfirmExclusion(active): %v", err)
	}
	lifecycle, _, _ := exclusionRowState(t, ids["active"])
	if lifecycle != LifecycleExcluded {
		t.Errorf("ConfirmExclusion(active): expected lifecycle %q, got %q", LifecycleExcluded, lifecycle)
	}
	for _, state := range []string{"done", "swept", "canceled", "merged", "excluded", "merged_excluded"} {
		before, _, _ := exclusionRowState(t, ids[state])
		// Why: every non-active state fails the ConfirmExclusion predicate (affected=0),
		// which surfaces as sql.ErrNoRows -- that IS the expected no-op signal here.
		if err := ConfirmExclusion(ctx, GetDB(), email, ids[state]); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ConfirmExclusion(%s): expected sql.ErrNoRows, got %v", state, err)
		}
		after, _, _ := exclusionRowState(t, ids[state])
		if before != after {
			t.Errorf("ConfirmExclusion(%s): lifecycle changed from %q to %q, want no-op", state, before, after)
		}
	}
}

func TestConsolidatedMessage_IsActiveMatchesLifecycleColumn(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("isactive")
	room := "general"
	threadID := "thread-isactive"
	ids := seedAllLifecycleStates(t, email, room, threadID)

	for _, f := range lifecycleFixtures {
		id := ids[f.state]
		m := loadConsolidatedForLifecycle(t, id)
		lifecycle, _, _ := exclusionRowState(t, id)
		want := lifecycle == LifecycleActive
		if got := m.IsActive(); got != want {
			t.Errorf("state %s: IsActive()=%v, but lifecycle column=%q", f.state, got, lifecycle)
		}
	}
}

// loadConsolidatedForLifecycle builds the minimal ConsolidatedMessage fields messageLifecycle
// reads (category, done, is_deleted, excluded_at), straight off the messages table.
// Why not a store query: every existing read path projects its own fixed column set,
// so routing this table test through any single one of them would test that query's
// projection, not IsActive() itself.
func loadConsolidatedForLifecycle(t *testing.T, id MessageID) ConsolidatedMessage {
	t.Helper()
	var (
		category   string
		done       bool
		isDeleted  bool
		excludedAt sql.NullString
	)
	err := GetDB().QueryRow(
		`SELECT COALESCE(category,''), done, is_deleted, excluded_at FROM messages WHERE id = ?`,
		int64(id),
	).Scan(&category, &done, &isDeleted, &excludedAt)
	if err != nil {
		t.Fatalf("loadConsolidatedForLifecycle(%d): %v", id, err)
	}
	m := ConsolidatedMessage{ID: id, Category: category, Done: done, IsDeleted: isDeleted}
	if excludedAt.Valid {
		ts, err := time.Parse(time.RFC3339, excludedAt.String)
		if err != nil {
			t.Fatalf("parse excluded_at %q: %v", excludedAt.String, err)
		}
		m.ExcludedAt = &ts
	}
	return m
}

// TestGetMessagesByIDs_CarriesExcludedAt guards against a regression where GetMessagesByIDs
// (and its toConsolidatedFromByIDs mapper) dropped excluded_at, leaving ConsolidatedMessage.IsActive()
// misclassifying an excluded row as active for every caller that loads by ID.
func TestGetMessagesByIDs_CarriesExcludedAt(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("getbyids-excluded")
	room := "general"
	threadID := "thread-getbyids-excluded"
	ids := seedAllLifecycleStates(t, email, room, threadID)

	msgs, err := GetMessagesByIDs(ctx, GetDB(), email, []MessageID{ids["active"], ids["excluded"]})
	if err != nil {
		t.Fatalf("GetMessagesByIDs: %v", err)
	}
	byID := make(map[MessageID]ConsolidatedMessage, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}

	excluded, ok := byID[ids["excluded"]]
	if !ok {
		t.Fatalf("GetMessagesByIDs: excluded row missing from result")
	}
	if excluded.ExcludedAt == nil {
		t.Errorf("GetMessagesByIDs(excluded): ExcludedAt = nil, want non-nil")
	}
	if excluded.IsActive() {
		t.Errorf("GetMessagesByIDs(excluded): IsActive() = true, want false")
	}

	active, ok := byID[ids["active"]]
	if !ok {
		t.Fatalf("GetMessagesByIDs: active row missing from result")
	}
	if active.ExcludedAt != nil {
		t.Errorf("GetMessagesByIDs(active): ExcludedAt = %v, want nil", active.ExcludedAt)
	}
	if !active.IsActive() {
		t.Errorf("GetMessagesByIDs(active): IsActive() = false, want true")
	}
}
