package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- HandleTelegramAuthStart input validation ----

// TestHandleTelegramAuthStart_BadJSON exercises the bad JSON 400 path.
func TestHandleTelegramAuthStart_BadJSON(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := httptest.NewRequest("POST", "/api/channels/telegram/auth/start", strings.NewReader("notjson"))
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleTelegramAuthStart(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleTelegramAuthStart_EmptyPhone exercises the empty phone 400 path.
func TestHandleTelegramAuthStart_EmptyPhone(t *testing.T) {
	t.Parallel()
	api := &API{}
	body := strings.NewReader(`{"phone":""}`)
	req := httptest.NewRequest("POST", "/api/channels/telegram/auth/start", body)
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleTelegramAuthStart(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleGmailDisconnect with DB ----

// TestHandleGmailDisconnect_WithDB exercises the store.DeleteGmailToken happy path.
func TestHandleGmailDisconnect_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "gmaildisconnect@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("POST", "/api/channels/gmail/disconnect", email)
	rr := httptest.NewRecorder()
	api.HandleGmailDisconnect(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleTelegramLogout with DB ----

// TestHandleTelegramLogout_WithDB exercises the LogoutTelegram happy path (no session → nil error).
func TestHandleTelegramLogout_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "tglogout@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("POST", "/api/channels/telegram/logout", email)
	rr := httptest.NewRecorder()
	api.HandleTelegramLogout(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleMergeTasks bad JSON path ----

// TestHandleMergeTasks_BadJSON exercises the decodeJSON 400 path.
func TestHandleMergeTasks_BadJSON(t *testing.T) {
	t.Parallel()
	api := &API{}
	req, _ := http.NewRequest("POST", "/api/messages/merge", bytes.NewBufferString("not-json"))
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleMergeTasks(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleMergeTasks_MissingIDs exercises the empty ids 400 path.
func TestHandleMergeTasks_MissingIDs(t *testing.T) {
	t.Parallel()
	api := &API{}
	body, _ := json.Marshal(map[string]interface{}{"target_ids": []int{}, "destination_id": 0})
	req, _ := http.NewRequest("POST", "/api/messages/merge", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleMergeTasks(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleRestoreGmailCC error path ----

// TestHandleRestoreGmailCC_NoToken exercises the Gmail service error path (no token in test DB).
func TestHandleRestoreGmailCC_NoToken(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "restoregmailcc@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("POST", "/api/actions/restore-gmail-cc", email)
	rr := httptest.NewRecorder()
	api.HandleRestoreGmailCC(rr, req)
	// GetGmailService returns error (no token) → handleAPIError → 500.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 body=%s", rr.Body.String())
	}
}

// ---- HandleTelegramAuthStart with valid phone ----

// TestHandleTelegramAuthStart_WithPhone exercises the StartTelegramAuth error path.
func TestHandleTelegramAuthStart_WithPhone(t *testing.T) {
	t.Parallel()
	api := &API{}
	body := strings.NewReader(`{"phone":"+15550001234"}`)
	req := httptest.NewRequest("POST", "/api/channels/telegram/auth/start", body)
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleTelegramAuthStart(rr, req)
	// StartTelegramAuth fails (no client initialized) → 500 or 503.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 body=%s", rr.Body.String())
	}
}

// ---- HandleGetStats with DB ----

// TestHandleGetStats_WithDB exercises the stats happy path.
func TestHandleGetStats_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "stats@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/stats", email)
	rr := httptest.NewRecorder()
	api.HandleGetStats(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}
