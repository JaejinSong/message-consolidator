package handlers

import (
	"context"
	"message-consolidator/auth"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// HandleListReports lists all reports for the current user.
func (a *API) HandleListReports(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	reports, err := store.ListReports(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, reports)
}

// HandleGenerateReport triggers the generation of a new report for a specific period.
// Why: Prevents double-billing and data redundancy by checking for existing reports within the same period before invoking the expensive AI generation process.
func (a *API) HandleGenerateReport(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	if start == "" || end == "" {
		respondError(w, http.StatusBadRequest, "Missing start or end date")
		return
	}

	// 1. Check for duplicate (same period)
	existing, err := store.GetReport(r.Context(), email, start, end)
	if err == nil && existing != nil {
		// Return existing report ID to client
		respondJSON(w, http.StatusConflict, map[string]interface{}{
			"error":     "Report for this period already exists",
			"report_id": existing.ID,
		})
		return
	}

	if a.Reports == nil {
		respondError(w, http.StatusServiceUnavailable, "Reports service not initialized")
		return
	}

	// 2. Generate New
	report, err := a.Reports.GenerateReport(r.Context(), email, start, end)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, report)
}

// HandleGetReportByID retrieves a specific report by its unique ID.
func (a *API) HandleGetReportByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id64, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil { // Guard Clause 1: Explicit Integer Conversion 방어
		respondError(w, http.StatusBadRequest, "Invalid report ID format")
		return
	}
	id := int(id64)
	email := auth.GetUserEmail(r)

	report, err := store.GetReportByID(r.Context(), id, email)
	if err != nil { // Guard Clause 2: Not Found 방어
		respondError(w, http.StatusNotFound, "Report not found")
		return
	}

	if report.UserEmail != email { // Guard Clause 3: 권한 방어
		respondError(w, http.StatusForbidden, "Forbidden access")
		return
	}

	respondJSON(w, http.StatusOK, report)
}

// HandleDeleteReport removes a report from the database.
func (a *API) HandleDeleteReport(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id64, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid report ID format")
		return
	}
	id := int(id64)
	email := auth.GetUserEmail(r)

	report, err := store.GetReportByID(r.Context(), id, email)
	if err != nil || report.UserEmail != email {
		respondError(w, http.StatusForbidden, "Report not found or Forbidden")
		return
	}

	if err := store.DeleteReport(r.Context(), id, email); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleTranslateReport handles the on-demand translation request for a report.
// Why: Implements a Just-in-Time (JIT) translation workflow using context timeouts to prevent goroutine leaks during AI processing.
func (a *API) HandleTranslateReport(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	vars := mux.Vars(r)
	idStr := vars["id"]
	id64, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid report ID format")
		return
	}
	id := int(id64)

	lang := r.URL.Query().Get("lang")
	if lang == "" {
		respondError(w, http.StatusBadRequest, "Missing lang parameter")
		return
	}

	if a.Reports == nil {
		respondError(w, http.StatusServiceUnavailable, "Reports service not initialized")
		return
	}

	// AI 번역은 시간이 걸릴 수 있으므로 30초 타임아웃을 설정합니다.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	summary, err := a.Reports.ProcessOnDemandTranslation(ctx, email, id, lang)
	if err != nil {
		logger.Errorf("[API] Translation failed for report %d (%s): %v", id, lang, err)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"language_code": lang,
		"summary":       summary,
	})
}
