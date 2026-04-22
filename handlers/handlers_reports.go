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
func (a *API) HandleGenerateReport(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	start, end, lang, source, done, err := a.parseGenerateReportParams(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if a.Reports == nil {
		respondError(w, http.StatusServiceUnavailable, "Reports service unavailable")
		return
	}

	report, err := a.Reports.GenerateReport(r.Context(), email, start, end, lang, source, done)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.respondWithReportStatus(w, report)
}

func (a *API) parseGenerateReportParams(r *http.Request) (string, string, string, *string, *bool, error) {
	q := r.URL.Query()
	start, end, lang := q.Get("start"), q.Get("end"), q.Get("lang")

	if _, err := time.Parse("2006-01-02", start); err != nil {
		return "", "", "", nil, nil, httpError("Invalid start date")
	}
	if lang == "" {
		return "", "", "", nil, nil, httpError("Missing lang parameter")
	}

	var sourcePtr *string
	if s := q.Get("channelId"); s != "" {
		sourcePtr = &s
	}

	var donePtr *bool
	if status := q.Get("status"); status != "" {
		val := status == "resolve"
		donePtr = &val
	}

	return start, end, lang, sourcePtr, donePtr, nil
}

func (a *API) respondWithReportStatus(w http.ResponseWriter, report *store.Report) {
	status := http.StatusAccepted
	if report.Status == store.ReportStatusCompleted {
		status = http.StatusOK
	}
	respondJSON(w, status, report)
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
	if lang == "" {
		respondError(w, http.StatusBadRequest, "Missing lang parameter")
		return
	}

	a.processReportTranslation(w, r, id, lang)
}

func (a *API) processReportTranslation(w http.ResponseWriter, r *http.Request, id int, lang string) {
	if a.Reports == nil {
		respondError(w, http.StatusServiceUnavailable, "Reports service unavailable")
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
	idStr := vars["id"]
	if idStr == "" {
		return 0, httpError("Missing ID")
	}
	id64, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return int(id64), nil
}

type httpErr string

func (e httpErr) Error() string { return string(e) }
func httpError(s string) error  { return httpErr(s) }
