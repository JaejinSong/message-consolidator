package handlers

import (
	"message-consolidator/auth"
	"net/http"
)

// HandleWeeklyReportTest dispatches a one-off weekly report email to the authenticated user (or a provided recipient).
func (a *API) HandleWeeklyReportTest(w http.ResponseWriter, r *http.Request) {
	if a.WeeklyDispatch == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "weekly report not configured"})
		return
	}
	recipient := auth.GetUserEmail(r)
	if err := a.WeeklyDispatch(r.Context(), recipient); err != nil {
		handleAPIError(w, r, err, "[WEEKLY-TEST]", "Failed to dispatch weekly report")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
