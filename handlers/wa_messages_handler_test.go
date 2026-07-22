package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func TestWaQueryAuth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configured string
		header     string
		wantOK     bool
	}{
		{"valid token", "s3cret", "Bearer s3cret", true},
		{"wrong token", "s3cret", "Bearer nope", false},
		{"missing header", "s3cret", "", false},
		{"unconfigured rejects all", "", "Bearer s3cret", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			mw := waQueryAuth(tt.configured)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest("GET", "/api/wa/messages", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rr := httptest.NewRecorder()
			mw.ServeHTTP(rr, req)
			if called != tt.wantOK {
				t.Errorf("handler called = %v, want %v", called, tt.wantOK)
			}
			if tt.wantOK && rr.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rr.Code)
			}
			if !tt.wantOK && rr.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rr.Code)
			}
		})
	}
}

func TestHandleListWAMessages_ClampsPagination(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	api := &API{}
	tests := []struct {
		name  string
		query string
	}{
		{"negative limit", "limit=-1&offset=-5"},
		{"huge limit", "limit=99999999"},
		{"garbage", "limit=abc&offset=xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/wa/messages?"+tt.query, nil)
			rr := httptest.NewRecorder()
			api.HandleListWAMessages(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			var body struct {
				Offset int64 `json:"offset"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("body not JSON: %v", err)
			}
			if body.Offset < 0 {
				t.Errorf("offset = %d, want >= 0", body.Offset)
			}
		})
	}
}
