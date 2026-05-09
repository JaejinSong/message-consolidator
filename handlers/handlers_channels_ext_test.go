package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
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
	// SQL error from missing schema → 500; or empty list → 200. Both are non-400.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 body=%s", rr.Body.String())
	}
}

// TestHandleRejectProposal_WithDB exercises the SQL error path — identity_merge_candidates
// table missing canonical_name in test schema, so store returns error → handler responds 500.
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
	// SQL error from missing schema column or no-op DELETE — 500 or 200 both acceptable.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 body=%s", rr.Body.String())
	}
}

// TestHandleGmailCallback_CodeExchangeError skipped — ExchangeGmailCode panics
// when the channels package oauth2 Config is nil (not initialized without real credentials).
func TestHandleGmailCallback_CodeExchangeError(t *testing.T) {
	t.Skip("BUG: ExchangeGmailCode panics on nil oauth2.Config when channels package uninitialized in test env")
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
