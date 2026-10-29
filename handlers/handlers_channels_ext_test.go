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

	"github.com/gorilla/mux"
)

// jsonBody serializes v to JSON and returns an io.Reader for use as a request body.
func jsonBody(v interface{}) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// TestHandleWhatsAppQR_NoClient verifies the error path when no WA client exists.
func TestHandleWhatsAppQR_NoClient(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := NewMockRequest("GET", "/api/whatsapp/qr", "waqr@example.com")
	rr := httptest.NewRecorder()
	api.HandleWhatsAppQR(rr, req)
	// No client initialized -> error -> 500.
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleWhatsAppLogout_NoClient verifies the error path when no WA client exists.
func TestHandleWhatsAppLogout_NoClient(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := NewMockRequest("POST", "/api/whatsapp/logout", "walogout@example.com")
	rr := httptest.NewRecorder()
	api.HandleWhatsAppLogout(rr, req)
	// No client initialized -> error -> 500.
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleListProposals_WithDB exercises the SQL error path when proposal schema is missing.
// ListPendingProposalGroups returns SQL error → handler responds with 500.
// TestHandleListProposals_WithDB verifies a fresh schema answers an empty proposal list with 200 and [].
func TestHandleListProposals_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "listproposals@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/identity/proposals", email)
	rr := httptest.NewRecorder()
	api.HandleListProposals(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
		t.Errorf("body = %s, want []", got)
	}
}

// TestHandleRejectProposal_WithDB exercises the SQL error path — identity_merge_candidates
// table missing canonical_name in test schema, so store returns error → handler responds 500.
// TestHandleRejectProposal_WithDB verifies rejecting an unknown group is an idempotent 200 on a fresh schema.
func TestHandleRejectProposal_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "rejectproposal@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := httptest.NewRequest("POST", "/api/identity/proposals/grp1/reject", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "grp1"})
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api.HandleRejectProposal(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleGmailCallback_CodeExchangeError verifies a rejected authorization code yields 500 instead of a stored token.
func TestHandleGmailCallback_CodeExchangeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	withGmailOAuthConfig(t, srv.URL)

	api := &API{}
	req := httptest.NewRequest("GET", "/auth/gmail/callback?state=gmail:u@x.io&code=bad", nil)
	rr := httptest.NewRecorder()
	api.HandleGmailCallback(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Token exchange failed") {
		t.Errorf("body = %s, want token exchange failure", rr.Body.String())
	}
}

// TestHandleTelegramSetCredentials_WithDB exercises the success path (saves to DB).
func TestHandleTelegramSetCredentials_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "tgcreds@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := httptest.NewRequest("POST", "/api/telegram/credentials",
		jsonBody(map[string]interface{}{"app_id": 12345, "app_hash": "abc123def456"}))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api.HandleTelegramSetCredentials(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}
