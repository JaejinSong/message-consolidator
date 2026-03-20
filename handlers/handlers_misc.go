package handlers

import (
	"message-consolidator/logger"
	"net/http"
	"os"
)

func HandleGetReleaseNotes(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("./RELEASE_NOTES_USER.md")
	if err != nil {
		logger.Errorf("Failed to read RELEASE_NOTES_USER.md: %v", err)
		http.Error(w, "Failed to load release notes", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]string{"content": string(data)})
}
