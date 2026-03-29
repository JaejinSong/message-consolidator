package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/store"
	"net/http"
	"strconv"

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
	idStr := vars["id"]
	id64, _ := strconv.ParseInt(idStr, 10, 64)
	id := int(id64)

	email := auth.GetUserEmail(r)
	report, err := store.GetReportByID(r.Context(), id, email)
	if err != nil {
		respondError(w, http.StatusNotFound, "Report not found")
		return
	}

	// Safety check already done in GetReportByID via email filtering
	if report.UserEmail != email {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	respondJSON(w, http.StatusOK, report)
}

// HandleDeleteReport removes a report from the database.
func (a *API) HandleDeleteReport(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]
	id64, _ := strconv.ParseInt(idStr, 10, 64)
	id := int(id64)

	email := auth.GetUserEmail(r)
	// Verify ownership before delete
	report, err := store.GetReportByID(r.Context(), id, email)
	if err != nil {
		respondError(w, http.StatusNotFound, "Report not found")
		return
	}
	email = auth.GetUserEmail(r)
	if report.UserEmail != email {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	err = store.DeleteReport(r.Context(), id, email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
