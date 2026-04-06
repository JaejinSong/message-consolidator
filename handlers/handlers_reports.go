package handlers

import (
	"context"
	"message-consolidator/auth"
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

// HandleGetReportHistory returns a lightweight list of reports for the history sidebar.
func (a *API) HandleGetReportHistory(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	reports, err := store.GetReportList(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, reports)
}

// HandleGenerateReport triggers the generation of a new report for a specific period.
// Why: Prevents double-billing and data redundancy by checking for existing reports.
// Idempotency: Returns 200 OK if the report already exists for the given date.
func (a *API) HandleGenerateReport(w http.ResponseWriter, r *http.Request) {
	email, start, end := auth.GetUserEmail(r), r.URL.Query().Get("start"), r.URL.Query().Get("end")
	if _, err := time.Parse("2006-01-02", start); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid start date format (YYYY-MM-DD required)")
		return
	}
	if _, err := time.Parse("2006-01-02", end); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid end date format (YYYY-MM-DD required)")
		return
	}

	lang := r.URL.Query().Get("lang")
	if lang == "" {
		respondError(w, http.StatusBadRequest, "lang parameter is required")
		return
	}

	if a.Reports == nil {
		respondError(w, http.StatusServiceUnavailable, "Reports service not initialized")
		return
	}

	if existing, err := store.GetReportByDate(r.Context(), email, start); err == nil && existing != nil {
		respondJSON(w, http.StatusOK, existing)
		return
	}

	report, err := a.Reports.GenerateReport(r.Context(), email, start, end, lang)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, report)
}

// HandleGetReportByID retrieves a specific report by its unique ID.
func (a *API) HandleGetReportByID(w http.ResponseWriter, r *http.Request) {
	id, err := a.parseReportID(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid report ID format")
		return
	}

	email := auth.GetUserEmail(r)
	report, err := store.GetReportByID(r.Context(), id, email)
	if err != nil || report.UserEmail != email {
		respondError(w, http.StatusNotFound, "Report not found or Forbidden")
		return
	}

	respondJSON(w, http.StatusOK, report)
}

// HandleDeleteReport removes a report from the database.
func (a *API) HandleDeleteReport(w http.ResponseWriter, r *http.Request) {
	id, err := a.parseReportID(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid report ID format")
		return
	}

	email := auth.GetUserEmail(r)
	if _, err := store.GetReportByID(r.Context(), id, email); err != nil {
		respondError(w, http.StatusNotFound, "Report not found")
		return
	}

	if err := store.DeleteReport(r.Context(), id, email); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleTranslateReport handles the on-demand translation request for a report.
func (a *API) HandleTranslateReport(w http.ResponseWriter, r *http.Request) {
	id, err := a.parseReportID(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid report ID format")
		return
	}

	lang := r.URL.Query().Get("lang")
	if lang == "" || a.Reports == nil {
		respondError(w, http.StatusBadRequest, "Missing lang parameter or Service unavailable")
		return
	}

	email := auth.GetUserEmail(r)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	summary, err := a.Reports.ProcessOnDemandTranslation(ctx, email, id, lang)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"language_code": lang, "summary": summary})
}

func (a *API) parseReportID(r *http.Request) (int, error) {
	vars := mux.Vars(r)
	id64, err := strconv.ParseInt(vars["id"], 10, 64)
	return int(id64), err
}
