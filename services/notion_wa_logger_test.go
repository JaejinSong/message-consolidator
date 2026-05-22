package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// notionTestServer stubs Notion API:
// POST /v1/databases → {"id": "db-test-id"}
// POST /v1/pages      → {"id": "page-test-id"}
func notionTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/databases":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "db-test-id"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/pages":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-test-id"})
		default:
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		}
	}))
}

func newTestLogger(t *testing.T, srv *httptest.Server) *WANotionLogger {
	t.Helper()
	l := &WANotionLogger{
		token:           "test-token",
		parentPageID:    "parent-page-id",
		client:          &http.Client{Transport: &hostRewriter{base: srv.URL, inner: srv.Client().Transport}},
		getSettingFn:    func(_ context.Context, _ string) (string, bool) { return "", false },
		upsertSettingFn: func(_ context.Context, _, _, _ string) error { return nil },
		dbIDs:           make(map[string]string),
	}
	return l
}

type hostRewriter struct {
	base  string
	inner http.RoundTripper
}

func (h *hostRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(h.base, "http://")
	return h.inner.RoundTrip(req2)
}

// --- helpers ---

func TestNotionWAMonthKey(t *testing.T) {
	ts := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	if got := notionWAMonthKey(ts); got != "2026-05" {
		t.Errorf("expected 2026-05, got %q", got)
	}
}

func TestNotionWASettingKeyForMonth(t *testing.T) {
	if got := notionWASettingKeyForMonth("2026-05"); got != "notion_wa_database_id_2026_05" {
		t.Errorf("unexpected key %q", got)
	}
}

func TestNotionWADBTitleForMonth(t *testing.T) {
	if got := notionWADBTitleForMonth("2026-05"); got != "WhatsApp Messages — 2026-05" {
		t.Errorf("unexpected title %q", got)
	}
}

// --- titleFromBody / mentionOptions ---

func TestTitleFromBody(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"short", "hello", "hello"},
		{"exact limit", strings.Repeat("a", notionWATitleMaxLen), strings.Repeat("a", notionWATitleMaxLen)},
		{"over limit", strings.Repeat("a", notionWATitleMaxLen+1), strings.Repeat("a", notionWATitleMaxLen) + "…"},
		{"trims spaces", "  hi  ", "hi"},
		{"multibyte over", strings.Repeat("가", notionWATitleMaxLen+1), strings.Repeat("가", notionWATitleMaxLen) + "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleFromBody(tc.input); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMentionOptions(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := mentionOptions(nil); len(got) != 0 {
			t.Errorf("expected empty, got %v", got)
		}
	})
	t.Run("skips blank", func(t *testing.T) {
		got := mentionOptions([]string{"", "  ", "Alice"})
		if len(got) != 1 || got[0]["name"] != "Alice" {
			t.Errorf("unexpected %v", got)
		}
	})
	t.Run("truncates long name", func(t *testing.T) {
		n, _ := mentionOptions([]string{strings.Repeat("가", notionWAMentionMaxLen+5)})[0]["name"].(string)
		if utf8.RuneCountInString(n) != notionWAMentionMaxLen {
			t.Errorf("expected rune len %d, got %d", notionWAMentionMaxLen, utf8.RuneCountInString(n))
		}
	})
}

// --- buildRowProperties ---

func TestBuildRowProperties(t *testing.T) {
	ts := time.Date(2026, 5, 22, 14, 23, 0, 0, time.UTC)
	msg := types.RawMessage{
		Sender: "송재진", Text: "안녕하세요", Timestamp: ts,
		IsFromMe: true, HasAttachment: true, RepliedToUser: "Alice",
		MentionedNames: []string{"Bob"},
	}
	props := buildRowProperties(msg, "팀채널")

	t.Run("outgoing direction", func(t *testing.T) {
		inner, _ := props["Direction"].(map[string]any)["select"].(map[string]any)
		if inner["name"] != "Outgoing" {
			t.Errorf("expected Outgoing, got %v", inner["name"])
		}
	})
	t.Run("incoming direction", func(t *testing.T) {
		msg2 := msg
		msg2.IsFromMe = false
		inner, _ := buildRowProperties(msg2, "팀채널")["Direction"].(map[string]any)["select"].(map[string]any)
		if inner["name"] != "Incoming" {
			t.Errorf("expected Incoming, got %v", inner["name"])
		}
	})
	t.Run("timestamp RFC3339", func(t *testing.T) {
		d, _ := props["Timestamp"].(map[string]any)["date"].(map[string]any)
		if d["start"] != ts.Format(time.RFC3339) {
			t.Errorf("unexpected timestamp %v", d["start"])
		}
	})
	t.Run("checkbox", func(t *testing.T) {
		if props["HasAttachment"].(map[string]any)["checkbox"] != true {
			t.Error("expected HasAttachment true")
		}
	})
	t.Run("mentions", func(t *testing.T) {
		opts, _ := props["Mentions"].(map[string]any)["multi_select"].([]map[string]any)
		if len(opts) != 1 || opts[0]["name"] != "Bob" {
			t.Errorf("unexpected mentions %v", opts)
		}
	})
}

// --- Enabled / Receive ---

func TestWANotionLoggerEnabled(t *testing.T) {
	cases := []struct{ tok, pid string; want bool }{
		{"", "", false}, {"tok", "", false}, {"", "pid", false}, {"tok", "pid", true},
	}
	for _, tc := range cases {
		if NewWANotionLogger(tc.tok, tc.pid).Enabled() != tc.want {
			t.Errorf("Enabled(%q,%q) = %v", tc.tok, tc.pid, !tc.want)
		}
	}
}

func TestWANotionLoggerReceiveDisabled(t *testing.T) {
	l := NewWANotionLogger("", "")
	l.Receive("e", "j", types.RawMessage{Text: "x"})
	l.mu.Lock()
	n := len(l.pending)
	l.mu.Unlock()
	if n != 0 {
		t.Errorf("disabled logger should not enqueue, got %d", n)
	}
}

func TestWANotionLoggerReceivePendingCap(t *testing.T) {
	l := NewWANotionLogger("tok", "pid")
	for i := range notionWAMaxPending + 5 {
		l.Receive("e", "j", types.RawMessage{ID: string(rune('a' + i%26))})
	}
	l.mu.Lock()
	n := len(l.pending)
	l.mu.Unlock()
	if n != notionWAMaxPending {
		t.Errorf("expected cap %d, got %d", notionWAMaxPending, n)
	}
}

// --- flush ---

func TestWANotionLoggerFlush_EmptyQueue(t *testing.T) {
	srv := notionTestServer(t)
	defer srv.Close()
	newTestLogger(t, srv).flush(context.Background()) // must not panic
}

func TestWANotionLoggerFlush_CreatesMonthlyDB(t *testing.T) {
	var dbTitles []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/databases" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if arr, ok := body["title"].([]any); ok && len(arr) > 0 {
				if obj, ok := arr[0].(map[string]any); ok {
					if txt, ok := obj["text"].(map[string]any); ok {
						dbTitles = append(dbTitles, txt["content"].(string))
					}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "db-may"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-x"})
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	var upserted string
	l.upsertSettingFn = func(_ context.Context, _, v, _ string) error { upserted = v; return nil }

	ts := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	l.Receive("e@test.com", "jid", types.RawMessage{Text: "hi", Timestamp: ts})
	l.flush(context.Background())

	if len(dbTitles) != 1 || dbTitles[0] != "WhatsApp Messages — 2026-05" {
		t.Errorf("unexpected DB titles: %v", dbTitles)
	}
	if upserted != "db-may" {
		t.Errorf("expected upserted 'db-may', got %q", upserted)
	}
}

func TestWANotionLoggerFlush_TwoMonthsTwoDB(t *testing.T) {
	dbCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/databases" {
			dbCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": fmt.Sprintf("db-%d", dbCount)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-x"})
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	may := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	jun := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	l.Receive("e", "j", types.RawMessage{Text: "may msg", Timestamp: may})
	l.Receive("e", "j", types.RawMessage{Text: "jun msg", Timestamp: jun})
	l.flush(context.Background())

	if dbCount != 2 {
		t.Errorf("expected 2 databases created, got %d", dbCount)
	}
	l.mu.Lock()
	cached := len(l.dbIDs)
	l.mu.Unlock()
	if cached != 2 {
		t.Errorf("expected 2 cached dbIDs, got %d", cached)
	}
}

func TestWANotionLoggerFlush_SameMonthOneDB(t *testing.T) {
	dbCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/databases" {
			dbCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "db-may"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-x"})
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	ts := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	l.Receive("e", "j", types.RawMessage{Text: "a", Timestamp: ts})
	l.Receive("e", "j", types.RawMessage{Text: "b", Timestamp: ts.Add(time.Hour)})
	l.flush(context.Background())

	if dbCount != 1 {
		t.Errorf("expected 1 database for same month, got %d", dbCount)
	}
}

func TestWANotionLoggerFlush_UsesExistingDBFromSettings(t *testing.T) {
	dbCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/databases" {
			dbCalled = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "page-x"})
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	l.getSettingFn = func(_ context.Context, _ string) (string, bool) { return "existing-db-id", true }

	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	l.Receive("e", "j", types.RawMessage{Text: "hi", Timestamp: ts})
	l.flush(context.Background())

	if dbCalled {
		t.Error("should not create DB when settings has existing id")
	}
}

func TestWANotionLoggerFlush_RequeuesOnDBError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	l.Receive("e", "j", types.RawMessage{Text: "msg", Timestamp: ts})
	l.flush(context.Background())

	l.mu.Lock()
	n := len(l.pending)
	l.mu.Unlock()
	if n != 1 {
		t.Errorf("expected 1 requeued message, got %d", n)
	}
}

func TestWANotionLoggerFlush_InvalidatesCacheOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/pages" {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "db-x"})
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	month := "2026-05"
	l.mu.Lock()
	l.dbIDs[month] = "stale-db-id"
	l.mu.Unlock()

	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	l.Receive("e", "j", types.RawMessage{Text: "msg", Timestamp: ts})
	l.flush(context.Background())

	l.mu.Lock()
	_, cached := l.dbIDs[month]
	l.mu.Unlock()
	if cached {
		t.Error("expected dbIDs[2026-05] cleared after 404")
	}
}

func TestWANotionLoggerCreateDatabase_NoID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "database"})
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	_, err := l.createDatabase(context.Background(), "2026-05")
	if err == nil {
		t.Error("expected error when response has no id")
	}
}

func TestWANotionLoggerChatNameResolver(t *testing.T) {
	srv := notionTestServer(t)
	defer srv.Close()

	l := newTestLogger(t, srv)
	l.mu.Lock()
	l.dbIDs["2026-05"] = "db-test-id"
	l.mu.Unlock()

	var resolvedJID string
	l.ChatNameResolver = func(_, jid string) string { resolvedJID = jid; return "그룹명" }

	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	l.Receive("e@test.com", "jid@g.us", types.RawMessage{Text: "hi", Timestamp: ts})
	l.flush(context.Background())

	if resolvedJID != "jid@g.us" {
		t.Errorf("expected resolver with jid@g.us, got %q", resolvedJID)
	}
}

func TestWANotionLoggerStart_DisabledNoOp(t *testing.T) {
	l := NewWANotionLogger("", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { l.Start(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("Start() with disabled logger should return immediately")
	}
}

func TestWANotionLoggerStart_CancelFlushes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "db-x"})
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	l.mu.Lock()
	l.dbIDs["2026-05"] = "db-x"
	l.mu.Unlock()
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	l.Receive("e", "j", types.RawMessage{Text: "final", Timestamp: ts})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Start(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("Start() did not exit after ctx cancellation")
	}
}

func TestWANotionLoggerCall_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	_, err := l.call(context.Background(), http.MethodPost, "/v1/pages", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 error, got %v", err)
	}
}

func TestWANotionLoggerCall_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "not-json")
	}))
	defer srv.Close()

	l := newTestLogger(t, srv)
	_, err := l.call(context.Background(), http.MethodPost, "/v1/pages", map[string]any{})
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestNotionWADBSchema(t *testing.T) {
	schema := notionWADBSchema()
	for _, k := range []string{"Title", "Timestamp", "Sender", "Chat", "Direction", "Body", "ReplyTo", "HasAttachment", "Forwarded", "Mentions"} {
		if _, ok := schema[k]; !ok {
			t.Errorf("schema missing %q", k)
		}
	}
}
