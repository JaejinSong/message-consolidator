package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func newTestClef(t *testing.T, srv *httptest.Server, threshold float64) *ClefFilter {
	t.Helper()
	f, err := NewClefFilter("test-key", srv.URL, "clef:cloud", threshold)
	if err != nil {
		t.Fatalf("NewClefFilter: %v", err)
	}
	f.client = srv.Client()
	return f
}

func clefReply(actionable float64) string {
	return `{"model":"clef:cloud","answers":{"verdict":{"type":"choice","choice":"x","probabilities":{"actionable":` +
		strings.TrimRight(strings.TrimRight(formatFloat(actionable), "0"), ".") +
		`,"marketing":0.5,"chatter":0.1},"confidence":0.1}},"usage":{"input_tokens":12,"output_tokens":0}}`
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func TestNewClefFilter_EmptyKey(t *testing.T) {
	if _, err := NewClefFilter("", "http://x", "clef:cloud", 0.3); err == nil {
		t.Fatal("expected error for empty api key")
	}
}

func TestClefFilter_RequestShape(t *testing.T) {
	var gotPath, gotAuth string
	var gotReq clefRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = w.Write([]byte(clefReply(0.9)))
	}))
	defer srv.Close()

	noise, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{Email: "u@example.com", Source: "slack", Text: "hello"})
	if err != nil || noise {
		t.Fatalf("IsNoise = %v, %v; want false, nil", noise, err)
	}
	if gotPath != "/systemone" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotReq.Model != "clef:cloud" || gotReq.State != "hello" {
		t.Errorf("model/state = %q/%q", gotReq.Model, gotReq.State)
	}
	q := gotReq.Questions["verdict"]
	if q.Type != "choice" || q.Criteria["actionable"] == "" || len(q.Criteria) != 8 {
		t.Errorf("question = %+v", q)
	}
}

func TestClefFilter_ThresholdBoundary(t *testing.T) {
	tests := []struct {
		name       string
		actionable float64
		want       bool
	}{
		{"below threshold is noise", 0.29, true},
		{"above threshold is not noise", 0.31, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(clefReply(tt.actionable)))
			}))
			defer srv.Close()
			got, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{Email: "u@example.com", Source: "slack", Text: "x"})
			if err != nil || got != tt.want {
				t.Errorf("IsNoise = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestClefFilter_Non2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad criteria"}`))
	}))
	defer srv.Close()
	_, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{Email: "u@example.com", Source: "slack", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad criteria") {
		t.Fatalf("err = %v, want status and server message", err)
	}
}

func TestClefFilter_MalformedAnswers(t *testing.T) {
	bodies := map[string]string{
		"not json":            `nope`,
		"missing verdict":     `{"answers":{}}`,
		"missing actionable":  `{"answers":{"verdict":{"probabilities":{"chatter":0.9}}}}`,
		"missing probability": `{"answers":{"verdict":{}}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			if _, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{Email: "u@example.com", Source: "slack", Text: "x"}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestClefFilter_RuneSafeTruncation(t *testing.T) {
	var state string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req clefRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		state = req.State
		_, _ = w.Write([]byte(clefReply(0.9)))
	}))
	defer srv.Close()

	text := strings.Repeat("가", clefMaxStateRunes+50)
	if _, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{Email: "u@example.com", Source: "slack", Text: text}); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(state); n != clefMaxStateRunes || !utf8.ValidString(state) {
		t.Errorf("state runes = %d valid = %v", n, utf8.ValidString(state))
	}
}

func TestTopNonActionable(t *testing.T) {
	got := topNonActionable(map[string]float64{"actionable": 0.9, "chatter": 0.2, "marketing": 0.5})
	if got != "marketing" {
		t.Errorf("got %q", got)
	}
}

func TestClefFilter_ErrorMessageBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("e", 5000)))
	}))
	defer srv.Close()
	_, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{Email: "u@example.com", Source: "slack", Text: "x"})
	if err == nil || len(err.Error()) > 300 {
		t.Fatalf("err = %v, want bounded error", err)
	}
}

func TestClefFilter_RecordsScoreRows(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	tests := []struct {
		name        string
		p           float64
		wantNoise   bool
		wantVerdict string
	}{
		{"pass", 0.9, false, store.FilterVerdictPass},
		{"noise", 0.1, true, store.FilterVerdictNoise},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(clefReply(tt.p)))
			}))
			defer srv.Close()
			email := testutil.RandomEmail(tt.name)

			got, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{
				Email: email, Source: "slack", Room: "room-a", SourceTS: "ts-9", MessageRef: "ts-8,ts-9", Text: "hello",
			})
			if err != nil || got != tt.wantNoise {
				t.Fatalf("IsNoise = %v, %v; want %v, nil", got, err, tt.wantNoise)
			}

			rows, err := store.ListFilterScoresSince(context.Background(), email, time.Now().Add(-time.Hour))
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows = %d, err = %v; want 1", len(rows), err)
			}
			r := rows[0]
			if r.Verdict != tt.wantVerdict || r.PActionable != tt.p || r.Threshold != 0.3 || r.Provider != "clef" {
				t.Errorf("row = %+v", r)
			}
			if r.Room != "room-a" || r.SourceTs != "ts-9" || r.MessageRef != "ts-8,ts-9" || r.Text != "hello" {
				t.Errorf("meta not stored: %+v", r)
			}
			if tt.wantNoise && r.Category != "marketing" {
				t.Errorf("category = %q, want marketing", r.Category)
			}
		})
	}
}

func TestClefFilter_ScoreTextCappedAt8000Runes(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(clefReply(0.9)))
	}))
	defer srv.Close()
	email := testutil.RandomEmail("cap")

	_, err = newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{
		Email: email, Source: "slack", Text: strings.Repeat("가", 9000),
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListFilterScoresSince(context.Background(), email, time.Now().Add(-time.Hour))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %d, err = %v", len(rows), err)
	}
	if n := utf8.RuneCountInString(rows[0].Text); n != 8000 {
		t.Errorf("stored runes = %d, want 8000", n)
	}
}

func TestClefFilter_InsertFailureKeepsVerdict(t *testing.T) {
	// Why: no DB is initialised here, so the insert fails; the verdict must be unaffected.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(clefReply(0.1)))
	}))
	defer srv.Close()

	got, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), FilterInput{Email: "u@example.com", Source: "slack", Text: "x"})
	if err != nil || !got {
		t.Fatalf("IsNoise = %v, %v; want true, nil", got, err)
	}
}
