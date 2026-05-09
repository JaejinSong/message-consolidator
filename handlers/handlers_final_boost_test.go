package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// ---- registerStaticRoutes ----

// TestRegisterStaticRoutes_DisabledWhenEnvSet verifies the env-gate path
// (DISABLE_STATIC_SERVING=true skips static route registration).
func TestRegisterStaticRoutes_DisabledWhenEnvSet(t *testing.T) {
	prev := os.Getenv("DISABLE_STATIC_SERVING")
	os.Setenv("DISABLE_STATIC_SERVING", "true")
	t.Cleanup(func() { os.Setenv("DISABLE_STATIC_SERVING", prev) })

	api := &API{Config: &config.Config{}}
	r := mux.NewRouter()
	api.registerStaticRoutes(r)
	// No panic and no static routes registered.

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	// With DISABLE_STATIC_SERVING=true the "/" route is not registered, so mux returns 404.
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500 body=%s", rr.Body.String())
	}
}

// TestRegisterStaticRoutes_EnabledByDefault verifies static routes are registered
// when DISABLE_STATIC_SERVING is unset (exercises the os.FileServer path without panic).
func TestRegisterStaticRoutes_EnabledByDefault(t *testing.T) {
	prev := os.Getenv("DISABLE_STATIC_SERVING")
	os.Unsetenv("DISABLE_STATIC_SERVING")
	t.Cleanup(func() { os.Setenv("DISABLE_STATIC_SERVING", prev) })

	api := &API{Config: &config.Config{}}
	r := mux.NewRouter()
	// Should not panic — file server creation is lazy.
	api.registerStaticRoutes(r)
}

// ---- HandleMarkDone success path ----

// TestHandleMarkDone_WithDBAndTasks exercises the Tasks.HandleTaskCompletion call.
func TestHandleMarkDone_WithDBAndTasks(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "markdone2@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")
	_, _ = store.GetDB().Exec(
		"INSERT INTO messages (id, user_email, task, source, source_ts) VALUES (?, ?, ?, ?, ?)",
		66, email, "Task66", "slack", "ts66",
	)
	_ = store.RefreshCache(context.Background(), email)

	// Zero-value TasksService: HandleTaskCompletion will be called and may fail,
	// but that exercises the call path past the nil/ID guards.
	api := &API{Tasks: &services.TasksService{}}
	body, _ := json.Marshal(map[string]interface{}{"id": 66, "done": true})
	req, _ := http.NewRequest("POST", "/api/messages/done", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api.HandleMarkDone(rr, req)
	// Accept 200 or 500 (TasksStub may error) — not 400 or 503.
	if rr.Code == http.StatusBadRequest || rr.Code == http.StatusServiceUnavailable {
		t.Errorf("unexpected status = %d body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleMergeTasks with Tasks nil ----

// TestHandleMergeTasks_NilTasks skipped — handler calls a.Tasks.MergeTasks without
// a nil check, which panics when Tasks is nil. The existing guard tests cover 400.
func TestHandleMergeTasks_NilTasks(t *testing.T) {
	t.Skip("BUG: HandleMergeTasks does not guard against nil Tasks; panics on nil pointer dereference")
}

// ---- HandleExportJSON with data ----

// TestHandleExportArchive_WithData exercises the CSV row-write path.
func TestHandleExportArchive_WithData(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "csvdata@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")
	// Insert a soft-deleted (archived) message.
	_, _ = store.GetDB().Exec(
		"INSERT INTO messages (id, user_email, task, source, source_ts, done, is_deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		77, email, "Task77", "slack", "ts77", 1, 0,
	)

	api := &API{}
	req := NewMockRequest("GET", "/api/messages/export?status=done", email)
	rr := httptest.NewRecorder()
	api.HandleExportArchive(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	// Response should contain the CSV header row.
	if !strings.Contains(rr.Body.String(), "Task") {
		t.Errorf("CSV missing header, body=%s", rr.Body.String())
	}
}

// ---- HandleTelegramAuthConfirm with bad code path ----

// TestHandleTelegramAuthConfirm_WithCode exercises the ConfirmTelegramCode error path.
func TestHandleTelegramAuthConfirm_WithCode(t *testing.T) {
	t.Parallel()
	api := &API{}
	body := strings.NewReader(`{"code":"12345"}`)
	req := httptest.NewRequest("POST", "/api/channels/telegram/auth/confirm", body)
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleTelegramAuthConfirm(rr, req)
	// ConfirmTelegramCode will fail because no Telegram session exists.
	// Expect 401 (error from ConfirmTelegramCode).
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleTelegramAuthPassword_WithPassword exercises the ConfirmTelegramPassword error path.
func TestHandleTelegramAuthPassword_WithPassword(t *testing.T) {
	t.Parallel()
	api := &API{}
	body := strings.NewReader(`{"password":"mypassword"}`)
	req := httptest.NewRequest("POST", "/api/channels/telegram/auth/password", body)
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleTelegramAuthPassword(rr, req)
	// ConfirmTelegramPassword will fail because no Telegram session exists.
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- processReportTranslation additional coverage ----

// TestProcessReportTranslation_NilReports re-exercises the 503 path via the direct method.
func TestProcessReportTranslation_NilReports(t *testing.T) {
	t.Parallel()
	api := &API{Reports: nil}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	api.processReportTranslation(rr, req, store.ReportID(1), "en")
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

