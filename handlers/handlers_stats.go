package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/store"
	"net/http"
)

//Why: Retrieves processed activity statistics for the user based on their specific timezone to ensure dashboard visualisations are chronologically accurate.
func (a *API) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)

	tz := r.Header.Get("X-Timezone")
	if tz == "" {
		tz = "UTC"
	}

	stats, err := store.GetUserStats(email, tz)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, stats)
}
