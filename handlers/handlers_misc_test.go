package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleSlackStatus(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"connected when token set", "xoxb-fake", "connected"},
		{"disconnected when token empty", "", "disconnected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &API{Config: &config.Config{SlackToken: tt.token}}
			req := httptest.NewRequest("GET", "/api/channels/slack/status", nil)
			rr := httptest.NewRecorder()

			api.HandleSlackStatus(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}
			var body slackStatusResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid json: %v", err)
			}
			if body.Status != tt.want {
				t.Errorf("expected status=%q, got %q", tt.want, body.Status)
			}
			if body.Stale {
				t.Error("expected stale=false when no scan has ever run")
			}
		})
	}
}

// Why: table for the server-side staleness rule — mirrors TestBuildGmailStatus.
// stale requires BOTH a connected token AND a last_success older than the 31m
// threshold; absence of last_success is never stale.
func TestBuildSlackStatus(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	fresh := fmt.Sprintf("%d", now.Add(-5*time.Minute).Unix())
	old := fmt.Sprintf("%d", now.Add(-32*time.Minute).Unix())

	tests := []struct {
		name       string
		connected  bool
		lastTS     string
		wantStale  bool
		wantLastAt bool
	}{
		{"connected, never scanned", true, "", false, false},
		{"connected, fresh scan", true, fresh, false, true},
		{"connected, scan older than threshold", true, old, true, true},
		{"disconnected, old scan", false, old, false, true},
		{"connected, malformed timestamp", true, "not-a-unix-ts", false, false},
		{"connected, zero timestamp", true, "0", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSlackStatus(tt.connected, "U1", tt.lastTS, now)
			if got.Stale != tt.wantStale {
				t.Errorf("stale = %v, want %v", got.Stale, tt.wantStale)
			}
			if (got.LastScanAt > 0) != tt.wantLastAt {
				t.Errorf("last_scan_at = %d, want set=%v", got.LastScanAt, tt.wantLastAt)
			}
		})
	}
}

// Why: end-to-end pin of the /slack/status contract — a connected workspace whose last
// clean scan exceeded the threshold must answer {status:"connected", stale:true}. This
// regresses the 2026-09-17 incident where a bot removed from every channel kept the UI green.
func TestHandleSlackStatus_StaleAfterThreshold(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "slack-stale@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	oldTS := fmt.Sprintf("%d", time.Now().Add(-32*time.Minute).Unix())
	if err := store.UpdateLastScan(email, store.SourceSlack, store.ScanTargetLastSuccess, oldTS); err != nil {
		t.Fatalf("seed last_success: %v", err)
	}

	api := &API{Config: &config.Config{SlackToken: "xoxb-fake"}}
	req := NewMockRequest("GET", "/api/channels/slack/status", email)
	rr := httptest.NewRecorder()
	api.HandleSlackStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body slackStatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body.Status != "connected" {
		t.Errorf("status = %q, want connected", body.Status)
	}
	if !body.Stale {
		t.Error("expected stale=true for scan older than threshold")
	}
	if body.LastScanAt <= 0 {
		t.Errorf("last_scan_at = %d, want > 0", body.LastScanAt)
	}
}

func TestHandleGetReleaseNotes_Validation(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"invalid type rejected", "?type=hack&lang=en", http.StatusBadRequest},
		{"non-alpha lang rejected", "?type=user&lang=e1", http.StatusBadRequest},
		{"oversize lang rejected", "?type=user&lang=koor", http.StatusBadRequest},
	}
	api := &API{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/release-notes"+tt.query, nil)
			rr := httptest.NewRecorder()

			api.HandleGetReleaseNotes(rr, req)

			if rr.Code != tt.want {
				t.Errorf("expected %d, got %d (body=%s)", tt.want, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestHandleGetStats_EmptyUser(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	api := &API{}
	req := NewMockRequest("GET", "/api/stats", "stats@example.com")
	rr := httptest.NewRecorder()
	api.HandleGetStats(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("HandleGetStats status = %d, want 200", rr.Code)
	}
}
