package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/store"
	"net/http"
)

func HandleGetStats(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)

	tz := r.Header.Get("X-Timezone")
	if tz == "" {
		tz = "UTC"
	}

	stats, err := store.GetUserStats(email, tz)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, stats)
}
