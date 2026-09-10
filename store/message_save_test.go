package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestEncodeJSONArray_EmptyUsesSchemaDefault pins the "[]" fallback. Why: a nil slice
// marshals to "null", which is not a JSON array -- production carried "null" in the
// constraints column on every row (2026-09-10).
func TestEncodeJSONArray_EmptyUsesSchemaDefault(t *testing.T) {
	if got := encodeJSONArray[string](nil); got != "[]" {
		t.Errorf("nil slice = %q, want %q", got, "[]")
	}
	if got := encodeJSONArray([]string{}); got != "[]" {
		t.Errorf("empty slice = %q, want %q", got, "[]")
	}
	if got := encodeJSONArray([]string{"a", "b"}); got != `["a","b"]` {
		t.Errorf("populated slice = %q, want %q", got, `["a","b"]`)
	}
}

func TestEncodeJSONObject_EmptyUsesSchemaDefault(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"nil", nil, "{}"},
		{"empty", json.RawMessage(""), "{}"},
		{"whitespace", json.RawMessage("  "), "{}"},
		{"json null literal", json.RawMessage("null"), "{}"},
		{"populated", json.RawMessage(`{"ai_original":{"task":"x"}}`), `{"ai_original":{"task":"x"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodeJSONObject(tc.raw); got != tc.want {
				t.Errorf("encodeJSONObject(%q) = %q, want %q", string(tc.raw), got, tc.want)
			}
		})
	}
}

// TestToCreateMessageParams_JSONDefaults verifies the insert boundary never emits a
// value that SQLite's json_extract would reject.
func TestToCreateMessageParams_JSONDefaults(t *testing.T) {
	params := toCreateMessageParams(ConsolidatedMessage{UserEmail: "u@test.com", Task: "t"})
	checks := map[string]struct {
		got  sql.NullString
		want string
	}{
		"constraints":          {params.Constraints, "[]"},
		"source_channels":      {params.SourceChannels, "[]"},
		"consolidated_context": {params.ConsolidatedContext, "[]"},
		"subtasks":             {params.Subtasks, "[]"},
		"metadata":             {params.Metadata, "{}"},
	}
	for name, c := range checks {
		if !c.got.Valid || c.got.String != c.want {
			t.Errorf("%s = %q (valid=%v), want %q", name, c.got.String, c.got.Valid, c.want)
		}
	}
}

// TestNormalizeJSONDefaults repairs legacy rows and stays idempotent on re-run.
func TestNormalizeJSONDefaults(t *testing.T) {
	raw, err := sql.Open("sqlite", fmt.Sprintf("file:jsondefaults_%d?mode=memory", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	if _, err := raw.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_email TEXT, task TEXT,
		constraints TEXT DEFAULT '[]', metadata TEXT DEFAULT '{}',
		source_channels TEXT DEFAULT '[]', consolidated_context TEXT DEFAULT '[]',
		subtasks TEXT DEFAULT '[]'
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO messages
		(user_email, task, constraints, metadata, source_channels, consolidated_context, subtasks)
		VALUES
		('u@test.com', 'broken', 'null', '', 'null', 'null', NULL),
		('u@test.com', 'intact', '["a"]', '{"k":1}', '["slack"]', '["c"]', '[]')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx := context.Background()
	if err := normalizeJSONDefaults(ctx, raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var c, md, sc, cc, st string
	row := raw.QueryRow(`SELECT constraints, metadata, source_channels, consolidated_context, subtasks
		FROM messages WHERE task = 'broken'`)
	if err := row.Scan(&c, &md, &sc, &cc, &st); err != nil {
		t.Fatalf("scan repaired: %v", err)
	}
	for name, pair := range map[string][2]string{
		"constraints": {c, "[]"}, "metadata": {md, "{}"},
		"source_channels": {sc, "[]"}, "consolidated_context": {cc, "[]"}, "subtasks": {st, "[]"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("repaired %s = %q, want %q", name, pair[0], pair[1])
		}
	}

	row = raw.QueryRow(`SELECT constraints, metadata FROM messages WHERE task = 'intact'`)
	if err := row.Scan(&c, &md); err != nil {
		t.Fatalf("scan intact: %v", err)
	}
	if c != `["a"]` || md != `{"k":1}` {
		t.Errorf("intact row mutated: constraints=%q metadata=%q", c, md)
	}

	// Idempotency: a second run must leave no offending rows behind.
	if err := normalizeJSONDefaults(ctx, raw); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	var offenders int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM messages
		WHERE constraints IN ('', 'null') OR metadata IN ('', 'null')`).Scan(&offenders); err != nil {
		t.Fatalf("count offenders: %v", err)
	}
	if offenders != 0 {
		t.Errorf("offenders after re-run = %d, want 0", offenders)
	}
}
