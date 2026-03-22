package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/store"
	"net/http"
)

func HandleGetStats(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	stats, err := store.GetUserStats(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, stats)
}
