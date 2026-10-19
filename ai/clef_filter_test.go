package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
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

	noise, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), "u@example.com", "slack", "hello")
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
			got, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), "u@example.com", "slack", "x")
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
	_, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), "u@example.com", "slack", "x")
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
			if _, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), "u@example.com", "slack", "x"); err == nil {
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
	if _, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), "u@example.com", "slack", text); err != nil {
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
	_, err := newTestClef(t, srv, 0.3).IsNoise(context.Background(), "u@example.com", "slack", "x")
	if err == nil || len(err.Error()) > 300 {
		t.Fatalf("err = %v, want bounded error", err)
	}
}
