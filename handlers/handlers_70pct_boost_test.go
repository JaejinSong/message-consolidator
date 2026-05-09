package handlers

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- HandleListReports ----

// TestHandleListReports_WithDB verifies the empty-reports happy path.
func TestHandleListReports_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "listreports@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/reports", email)
	rr := httptest.NewRecorder()
	api.HandleListReports(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleGetReportHistory ----

// TestHandleGetReportHistory_WithDB verifies the empty history happy path.
func TestHandleGetReportHistory_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "historyreports@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/reports/history", email)
	rr := httptest.NewRecorder()
	api.HandleGetReportHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleDeleteReport ----

// TestHandleDeleteReport_InvalidID exercises the 400 path from bad ID format.
func TestHandleDeleteReport_InvalidID(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := withReportID(httptest.NewRequest("DELETE", "/api/reports/badid", nil), "badid", "u@x.io")
	rr := httptest.NewRecorder()
	api.HandleDeleteReport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleDeleteReport_NotFound exercises the 404 path for missing report.
func TestHandleDeleteReport_NotFound(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "delreport@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := withReportID(httptest.NewRequest("DELETE", "/api/reports/999", nil), "999", email)
	rr := httptest.NewRecorder()
	api.HandleDeleteReport(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleTranslateReport ----

// TestHandleTranslateReport_MissingLang exercises the missing lang 400 path.
func TestHandleTranslateReport_MissingLang(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := withReportID(httptest.NewRequest("POST", "/api/reports/1/translate", nil), "1", "u@x.io")
	rr := httptest.NewRecorder()
	api.HandleTranslateReport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleGetTenantAliases ----

// TestHandleGetTenantAliases_WithDB verifies the store read happy path.
func TestHandleGetTenantAliases_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "tenantaliases@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/contacts/aliases", email)
	rr := httptest.NewRecorder()
	api.HandleGetTenantAliases(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleGetMappings ----

// TestHandleGetMappings_WithDB verifies the store read happy path.
func TestHandleGetMappings_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "getmappings@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/contacts/mappings", email)
	rr := httptest.NewRecorder()
	api.HandleGetMappings(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleGetLinks ----

// TestHandleGetLinks_WithDB verifies the store read happy path.
func TestHandleGetLinks_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "getlinks@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/contacts/links", email)
	rr := httptest.NewRecorder()
	api.HandleGetLinks(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}
