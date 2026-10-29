package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"message-consolidator/internal/testutil"
)

const twoOpenSubtasks = `[{"task":"A","assignee_id":null,"assignee":"","done":false},{"task":"B","assignee_id":null,"assignee":"","done":false}]`

func seedSubtaskRow(t *testing.T, q Querier, email, subtasks string) int64 {
	t.Helper()
	res, err := q.Exec(`INSERT INTO messages (user_email, task, source, subtasks, done, metadata) VALUES (?, 'Parent', 'gmail', ?, 0, '{"completion_candidate":{"x":1},"keep":"v"}')`, email, subtasks)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

type subtaskRowState struct {
	subtasks    []Subtask
	done        int
	completedAt sql.NullTime
	metadata    string
	rawJSON     string
}

func readSubtaskRow(t *testing.T, q Querier, id int64) subtaskRowState {
	t.Helper()
	var s subtaskRowState
	if err := q.QueryRow(`SELECT COALESCE(subtasks,''), done, completed_at, COALESCE(metadata,'') FROM messages WHERE id = ?`, id).
		Scan(&s.rawJSON, &s.done, &s.completedAt, &s.metadata); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if err := json.Unmarshal([]byte(s.rawJSON), &s.subtasks); err != nil {
		t.Fatalf("stored subtasks must unmarshal into []Subtask: %v (raw=%s)", err, s.rawJSON)
	}
	return s
}

func setupSubtaskDB(t *testing.T) *sql.DB {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(cleanup)
	return GetDB()
}

func TestUpdateSubtaskStatus_SequentialToggleClosesParent(t *testing.T) {
	db := setupSubtaskDB(t)
	ctx := context.Background()
	id := seedSubtaskRow(t, db, "a@example.com", twoOpenSubtasks)

	for i := 0; i < 2; i++ {
		if err := UpdateSubtaskStatus(ctx, db, "a@example.com", MessageID(id), i, true); err != nil {
			t.Fatalf("toggle %d: %v", i, err)
		}
	}

	s := readSubtaskRow(t, db, id)
	if !s.subtasks[0].Done || !s.subtasks[1].Done {
		t.Errorf("both subtasks should be done: %+v", s.subtasks)
	}
	if s.done != 1 || !s.completedAt.Valid {
		t.Errorf("parent should be closed with completed_at: done=%d completed_at=%v", s.done, s.completedAt)
	}
	var meta map[string]any
	_ = json.Unmarshal([]byte(s.metadata), &meta)
	if _, ok := meta["completion_candidate"]; ok || meta["keep"] != "v" {
		t.Errorf("completion_candidate must be removed and other keys kept: %s", s.metadata)
	}
	var confirmed sql.NullTime
	_ = db.QueryRow(`SELECT confirmed_at FROM messages WHERE id = ?`, id).Scan(&confirmed)
	if !confirmed.Valid {
		t.Error("confirmed_at should be set on auto-close")
	}
}

func TestUpdateSubtaskStatus_PartialKeepsParentOpen(t *testing.T) {
	db := setupSubtaskDB(t)
	id := seedSubtaskRow(t, db, "a@example.com", twoOpenSubtasks)

	if err := UpdateSubtaskStatus(context.Background(), db, "a@example.com", MessageID(id), 1, true); err != nil {
		t.Fatalf("toggle: %v", err)
	}

	s := readSubtaskRow(t, db, id)
	if s.subtasks[0].Done || !s.subtasks[1].Done {
		t.Errorf("only subtask 1 should be done: %+v", s.subtasks)
	}
	if s.done != 0 || s.completedAt.Valid {
		t.Errorf("parent must stay open: done=%d completed_at=%v", s.done, s.completedAt)
	}
}

func TestUpdateSubtaskStatus_BadIndexOrOwnerChangesNothing(t *testing.T) {
	db := setupSubtaskDB(t)
	id := seedSubtaskRow(t, db, "a@example.com", twoOpenSubtasks)
	nullID := seedSubtaskRow(t, db, "a@example.com", "[]")
	if _, err := db.Exec(`UPDATE messages SET subtasks = NULL WHERE id = ?`, nullID); err != nil {
		t.Fatalf("null seed: %v", err)
	}

	cases := []struct {
		name  string
		email string
		id    int64
		idx   int
	}{
		{"index past end", "a@example.com", id, 2},
		{"negative index", "a@example.com", id, -1},
		{"other user", "b@example.com", id, 0},
		{"missing row", "a@example.com", 987654, 0},
		{"NULL subtasks", "a@example.com", nullID, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := UpdateSubtaskStatus(context.Background(), db, c.email, MessageID(c.id), c.idx, true)
			if err == nil {
				t.Fatal("expected error")
			}
			s := readSubtaskRow(t, db, id)
			if s.rawJSON != twoOpenSubtasks || s.done != 0 {
				t.Errorf("row must be unchanged: %s done=%d", s.rawJSON, s.done)
			}
		})
	}
}

func TestUpdateSubtaskStatus_UncheckAfterCloseDoesNotReopen(t *testing.T) {
	db := setupSubtaskDB(t)
	ctx := context.Background()
	id := seedSubtaskRow(t, db, "a@example.com", twoOpenSubtasks)
	for i := 0; i < 2; i++ {
		if err := UpdateSubtaskStatus(ctx, db, "a@example.com", MessageID(id), i, true); err != nil {
			t.Fatalf("toggle %d: %v", i, err)
		}
	}

	if err := UpdateSubtaskStatus(ctx, db, "a@example.com", MessageID(id), 0, false); err != nil {
		t.Fatalf("uncheck: %v", err)
	}

	s := readSubtaskRow(t, db, id)
	if s.subtasks[0].Done || !s.subtasks[1].Done {
		t.Errorf("subtask 0 should be undone: %+v", s.subtasks)
	}
	if s.done != 1 || !s.completedAt.Valid {
		t.Errorf("parent must stay done: done=%d completed_at=%v", s.done, s.completedAt)
	}
}

func TestUpdateSubtaskStatus_ConcurrentTogglesKeepBoth(t *testing.T) {
	ResetForTest()
	path := filepath.Join(t.TempDir(), "subtask_race.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	raw.SetMaxOpenConns(8)
	ctx := context.Background()
	if err := EnsureSchemaAndSeeds(ctx, raw); err != nil {
		t.Fatalf("schema: %v", err)
	}

	const iterations = 50
	for n := 0; n < iterations; n++ {
		id := seedSubtaskRow(t, raw, "a@example.com", twoOpenSubtasks)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = UpdateSubtaskStatus(ctx, raw, "a@example.com", MessageID(id), i, true)
			}(i)
		}
		wg.Wait()

		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("iter %d: toggle errors: %v %v", n, errs[0], errs[1])
		}
		s := readSubtaskRow(t, raw, id)
		if !s.subtasks[0].Done || !s.subtasks[1].Done || s.done != 1 || !s.completedAt.Valid {
			t.Fatalf("iter %d: lost update: %s done=%d", n, s.rawJSON, s.done)
		}
	}
}
