package handlers

import (
	"context"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsAlpha covers the false branch (non-alphabetic chars).
func TestIsAlpha(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"abc", true},
		{"ABC", true},
		{"AbCdEf", true},
		{"한글", true},   // unicode letters
		{"a1b", false}, // digit
		{"ab!", false}, // punctuation
		{" ab", false}, // space
		{"a-b", false}, // hyphen
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := isAlpha(tt.input); got != tt.want {
				t.Errorf("isAlpha(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestHandleGetReleaseNotes_DefaultsAndFallback exercises the valid-type
// path where the file does not exist (returns 500 after fallback).
// Why: tests the success-path branch guard where type/lang pass validation.
func TestHandleGetReleaseNotes_ValidParams_NoFile(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := httptest.NewRequest("GET", "/api/release-notes?type=user&lang=en", nil)
	rr := httptest.NewRecorder()
	api.HandleGetReleaseNotes(rr, req)
	// File likely absent in test env; handler should return 500 after fallback.
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 200 or 500", rr.Code)
	}
}

// TestHandleGetReleaseNotes_TechType exercises the TECH type path.
func TestHandleGetReleaseNotes_TechType(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := httptest.NewRequest("GET", "/api/release-notes?type=tech&lang=ko", nil)
	rr := httptest.NewRecorder()
	api.HandleGetReleaseNotes(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 200 or 500", rr.Code)
	}
}

// TestHandleSlackStatus_WithUserEmail exercises the path that loads user from DB for slack_id.
func TestHandleSlackStatus_WithUserEmail(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "slack-status@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{Config: &config.Config{SlackToken: "xoxb-test"}}
	req := NewMockRequest("GET", "/api/channels/slack/status", email)
	rr := httptest.NewRecorder()
	api.HandleSlackStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}
