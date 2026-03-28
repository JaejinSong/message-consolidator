package handlers

import (
	"message-consolidator/auth"
	"net/http"
)

// HandleGetInsightReport returns the AI summary and visualization data for the user's weekly activity.
// Why: Provides a high-level productivity overview by combining backend-calculated relationship graphs with AI-generated narrative summaries.
func (a *API) HandleGetInsightReport(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	if email == "" {
		respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Why: Ensures the service is available since reports require a valid Gemini API key during initialization.
	if a.Reports == nil {
		respondError(w, http.StatusServiceUnavailable, "Reports service is not initialized (Check Gemini API Key)")
		return
	}

	report, err := a.Reports.GetWeeklyReport(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, report)
}
