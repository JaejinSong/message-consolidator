package store

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

const libsqlTestURLEnv = "LIBSQL_TEST_URL"

// Why: json_set/json_each and the stored `true` form must behave the same on a real libsql server as on local SQLite.
func setupLibsqlSubtaskDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv(libsqlTestURLEnv)
	if url == "" {
		t.Skipf("%s not set", libsqlTestURLEnv)
	}
	ResetForTest()
	conn, err := sql.Open("libsql", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := EnsureSchemaAndSeeds(context.Background(), conn); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return conn
}

func TestUpdateSubtaskStatus_LibsqlServer(t *testing.T) {
	conn := setupLibsqlSubtaskDB(t)
	ctx := context.Background()
	const email = "libsql@example.com"

	t.Run("sequential toggles close parent and keep stored form", func(t *testing.T) {
		id := seedSubtaskRow(t, conn, email, twoOpenSubtasks)
		for i := 0; i < 2; i++ {
			if err := UpdateSubtaskStatus(ctx, conn, email, MessageID(id), i, true); err != nil {
				t.Fatalf("toggle %d: %v", i, err)
			}
		}
		s := readSubtaskRow(t, conn, id)
		if !s.subtasks[0].Done || !s.subtasks[1].Done {
			t.Errorf("both subtasks should be done: %s", s.rawJSON)
		}
		if s.done != 1 || !s.completedAt.Valid {
			t.Errorf("parent should be closed: done=%d completed_at=%v", s.done, s.completedAt)
		}
	})

	t.Run("partial keeps parent open", func(t *testing.T) {
		id := seedSubtaskRow(t, conn, email, twoOpenSubtasks)
		if err := UpdateSubtaskStatus(ctx, conn, email, MessageID(id), 1, true); err != nil {
			t.Fatalf("toggle: %v", err)
		}
		s := readSubtaskRow(t, conn, id)
		if s.subtasks[0].Done || !s.subtasks[1].Done || s.done != 0 {
			t.Errorf("unexpected state: %s done=%d", s.rawJSON, s.done)
		}
	})

	t.Run("bad index and other owner change nothing", func(t *testing.T) {
		id := seedSubtaskRow(t, conn, email, twoOpenSubtasks)
		for _, c := range []struct {
			email string
			idx   int
		}{{email, 2}, {email, -1}, {"other@example.com", 0}} {
			if err := UpdateSubtaskStatus(ctx, conn, c.email, MessageID(id), c.idx, true); err == nil {
				t.Errorf("email=%s idx=%d: expected error", c.email, c.idx)
			}
		}
		if s := readSubtaskRow(t, conn, id); s.rawJSON != twoOpenSubtasks || s.done != 0 {
			t.Errorf("row must be unchanged: %s done=%d", s.rawJSON, s.done)
		}
	})

	t.Run("uncheck after close does not reopen", func(t *testing.T) {
		id := seedSubtaskRow(t, conn, email, twoOpenSubtasks)
		for i := 0; i < 2; i++ {
			if err := UpdateSubtaskStatus(ctx, conn, email, MessageID(id), i, true); err != nil {
				t.Fatalf("toggle %d: %v", i, err)
			}
		}
		if err := UpdateSubtaskStatus(ctx, conn, email, MessageID(id), 0, false); err != nil {
			t.Fatalf("uncheck: %v", err)
		}
		s := readSubtaskRow(t, conn, id)
		if s.subtasks[0].Done || !s.subtasks[1].Done || s.done != 1 {
			t.Errorf("unexpected state: %s done=%d", s.rawJSON, s.done)
		}
	})

	t.Run("concurrent toggles keep both", func(t *testing.T) {
		const iterations = 20
		for n := 0; n < iterations; n++ {
			id := seedSubtaskRow(t, conn, email, twoOpenSubtasks)
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					errs[i] = UpdateSubtaskStatus(ctx, conn, email, MessageID(id), i, true)
				}(i)
			}
			wg.Wait()
			if errs[0] != nil || errs[1] != nil {
				t.Fatalf("iter %d: toggle errors: %v %v", n, errs[0], errs[1])
			}
			s := readSubtaskRow(t, conn, id)
			if !s.subtasks[0].Done || !s.subtasks[1].Done || s.done != 1 {
				t.Fatalf("iter %d: lost update: %s done=%d", n, s.rawJSON, s.done)
			}
		}
	})
}
