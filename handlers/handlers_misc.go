package handlers

import (
	"message-consolidator/logger"
	"net/http"
	"os"
)

func (a *API) HandleGetReleaseNotes(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("./RELEASE_NOTES_USER.md")
	if err != nil {
		logger.Errorf("Failed to read RELEASE_NOTES_USER.md: %v", err)
		http.Error(w, "Failed to load release notes", http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"content": string(data)})
}

func (a *API) HandleSlackStatus(w http.ResponseWriter, r *http.Request) {
	status := "DISCONNECTED"
	if a.Config.SlackToken != "" {
		status = "CONNECTED"
	}
	logger.Debugf("[CHANNEL] Slack token status: %s", status)
	respondJSON(w, http.StatusOK, map[string]string{"status": status})
}
