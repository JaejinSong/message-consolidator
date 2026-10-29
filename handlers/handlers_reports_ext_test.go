package handlers

import (
	"encoding/json"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func seedReport(t *testing.T, email string) store.ReportID {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	t.Cleanup(cleanup)
	if _, err := store.GetOrCreateUser(t.Context(), email, "Owner", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	id, err := store.SaveReport(t.Context(), &store.Report{
		UserEmail: email, StartDate: "2024-06-01", EndDate: "2024-06-07", Visualization: "{}",
	})
	if err != nil {
		t.Fatalf("save report: %v", err)
	}
	return id
}

// TestRespondWithReportStatus covers both status branches.
func TestRespondWithReportStatus(t *testing.T) {
	t.Parallel()

	t.Run("completed report returns 200", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		rr := httptest.NewRecorder()
		report := &store.Report{Status: store.ReportStatusCompleted, ReportSummary: "done"}
		api.respondWithReportStatus(rr, report)
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})

	t.Run("pending report returns 202", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		rr := httptest.NewRecorder()
		report := &store.Report{Status: "pending", ReportSummary: "running"}
		api.respondWithReportStatus(rr, report)
		if rr.Code != http.StatusAccepted {
			t.Errorf("status = %d, want 202", rr.Code)
		}
	})
}

// TestParseGenerateReportParams covers the channelId and status branches.
func TestParseGenerateReportParams(t *testing.T) {
	t.Parallel()

	t.Run("valid params with channelId and status=resolve", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		req := httptest.NewRequest("GET", "/api/reports?start=2024-01-01&end=2024-01-07&lang=en&channelId=C123&status=resolve", nil)
		start, end, lang, src, done, err := api.parseGenerateReportParams(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != "2024-01-01" || end != "2024-01-07" || lang != "en" {
			t.Errorf("params: start=%q end=%q lang=%q", start, end, lang)
		}
		if src == nil || *src != "C123" {
			t.Errorf("src = %v, want C123", src)
		}
		if done == nil || !*done {
			t.Errorf("done = %v, want true", done)
		}
	})

	t.Run("valid params with status=open sets done=false", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		req := httptest.NewRequest("GET", "/api/reports?start=2024-01-01&end=2024-01-07&lang=ko&status=open", nil)
		_, _, _, _, done, err := api.parseGenerateReportParams(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if done == nil || *done {
			t.Errorf("done = %v, want false", done)
		}
	})

	t.Run("no channelId and no status leaves pointers nil", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		req := httptest.NewRequest("GET", "/api/reports?start=2024-01-01&lang=en", nil)
		_, _, _, src, done, err := api.parseGenerateReportParams(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if src != nil {
			t.Errorf("expected nil src, got %v", src)
		}
		if done != nil {
			t.Errorf("expected nil done, got %v", done)
		}
	})
}

// TestHandleDeleteReport_Success seeds a real report row to exercise the happy path.
func TestHandleDeleteReport_Success(t *testing.T) {
	id := seedReport(t, "u@x.io")
	api := &API{}
	req := withReportID(httptest.NewRequest("DELETE", "/api/reports/x", nil), strconv.FormatInt(int64(id), 10), "u@x.io")
	rr := httptest.NewRecorder()
	api.HandleDeleteReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
	if _, err := store.GetReportByID(t.Context(), id, "u@x.io"); err == nil {
		t.Error("report still exists after delete")
	}
}

// TestHandleGetReportByID_OwnerSuccess seeds a real report and verifies the 200 path.
func TestHandleGetReportByID_OwnerSuccess(t *testing.T) {
	id := seedReport(t, "u@x.io")
	api := &API{}
	idStr := strconv.FormatInt(int64(id), 10)

	rr := httptest.NewRecorder()
	api.HandleGetReportByID(rr, withReportID(httptest.NewRequest("GET", "/api/reports/x", nil), idStr, "u@x.io"))
	if rr.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
	var got store.Report
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != id || got.StartDate != "2024-06-01" || got.EndDate != "2024-06-07" {
		t.Errorf("report = %+v, want id=%d 2024-06-01..2024-06-07", got, id)
	}

	rr = httptest.NewRecorder()
	api.HandleGetReportByID(rr, withReportID(httptest.NewRequest("GET", "/api/reports/x", nil), idStr, "other@x.io"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("foreign user status = %d, want 404", rr.Code)
	}
}

// TestHandleListReports_JSONResponse ensures the response is a JSON array (not null/error).
// TestHandleGenerateReport_SuccessInvokesService verifies 503 when Reports is nil
// (already tested in handlers_reports_test.go). This exercises the end-to-end
// request parsing happy path (valid start/end/lang) leading to the service check.
func TestHandleGenerateReport_ValidParamsThenService(t *testing.T) {
	t.Parallel()
	api := &API{Reports: nil}
	req := httptest.NewRequest("GET", "/api/reports?start=2024-06-01&end=2024-06-07&lang=en", nil)
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleGenerateReport(rr, req)
	// nil Reports service must return 503 after validation passes
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

// TestHandleTranslateReport_ReportsNilAfterValidID verifies processReportTranslation
// returns 503 when Reports is nil (exercises the processReportTranslation function body).
func TestHandleTranslateReport_ReportsNilAfterValidID(t *testing.T) {
	t.Parallel()
	api := &API{Reports: nil}
	// ID "1" with valid lang; processReportTranslation is called and should return 503.
	req := withReportID(httptest.NewRequest("POST", "/api/reports/1/translate?lang=en", nil), "1", "u@x.io")
	rr := httptest.NewRecorder()
	api.HandleTranslateReport(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleExportReportToNotion_NotionDisabled exercises the "not configured" branch for an owned report.
func TestHandleExportReportToNotion_NotionDisabled(t *testing.T) {
	id := seedReport(t, "u@x.io")
	api := &API{Config: &config.Config{}}
	req := withReportID(httptest.NewRequest("POST", "/api/reports/x/notion", nil), strconv.FormatInt(int64(id), 10), "u@x.io")
	rr := httptest.NewRecorder()
	api.HandleExportReportToNotion(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Notion integration not configured") {
		t.Errorf("body = %s, want notion-not-configured message", rr.Body.String())
	}
}

// TestParseGenerateReportParams_EndParam verifies end= param is passed through correctly.
func TestParseGenerateReportParams_EndParam(t *testing.T) {
	t.Parallel()
	api := &API{}
	req := httptest.NewRequest("GET", "/api/reports?start=2024-01-01&end=2024-03-31&lang=ja", nil)
	start, end, lang, _, _, err := api.parseGenerateReportParams(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != "2024-01-01" {
		t.Errorf("start = %q", start)
	}
	if end != "2024-03-31" {
		t.Errorf("end = %q", end)
	}
	if lang != "ja" {
		t.Errorf("lang = %q", lang)
	}
}

// TestRespondWithReportStatus_BodyIsReport checks the serialized report is in the body.
func TestRespondWithReportStatus_BodyIsReport(t *testing.T) {
	t.Parallel()
	api := &API{}
	rr := httptest.NewRecorder()
	report := &store.Report{
		Status:        store.ReportStatusCompleted,
		ReportSummary: "summary text",
		StartDate:     "2024-01-01",
		EndDate:       "2024-01-07",
	}
	api.respondWithReportStatus(rr, report)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got store.Report
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ReportSummary != "summary text" {
		t.Errorf("summary = %q", got.ReportSummary)
	}
}
