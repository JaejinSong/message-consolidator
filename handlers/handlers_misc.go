package handlers

import (
	"fmt"
	"message-consolidator/logger"
	"net/http"
	"os"
	"strings"
	"unicode"
)

// isAlpha checks if a string contains only alphabetic characters.
func isAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// Why: Reads release notes from the filesystem, supporting different types (user, tech) and languages (ko, en) to provide targeted updates.
func (a *API) HandleGetReleaseNotes(w http.ResponseWriter, r *http.Request) {
	noteType := r.URL.Query().Get("type")
	if noteType == "" {
		noteType = "user" // Default to user-facing notes
	}

	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "en" // Default to English
	}

	// Sanitize inputs to prevent path traversal
	noteType = strings.ToUpper(noteType)
	lang = strings.ToUpper(lang)

	if noteType != "USER" && noteType != "TECH" {
		respondError(w, http.StatusBadRequest, "Invalid type parameter. Must be 'user' or 'tech'.")
		return
	}
	if len(lang) > 3 || !isAlpha(lang) {
		respondError(w, http.StatusBadRequest, "Invalid lang parameter.")
		return
	}

	fileName := fmt.Sprintf("./RELEASE_NOTES_%s_%s.md", noteType, lang)

	data, err := os.ReadFile(fileName)
	if os.IsNotExist(err) {
		// Fallback to English if the requested language is not found
		logger.Warnf("Release note for lang '%s' not found, falling back to EN.", lang)
		fallbackFileName := fmt.Sprintf("./RELEASE_NOTES_%s_EN.md", noteType)
		data, err = os.ReadFile(fallbackFileName)
	}

	if err != nil {
		logger.Errorf("Failed to read release notes file %s (or its fallback): %v", fileName, err)
		respondError(w, http.StatusInternalServerError, "Failed to load release notes")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"content": string(data)})
}

// Why: Checks the presence of the Slack API token to determine the current connection status of the Slack integration.
func (a *API) HandleSlackStatus(w http.ResponseWriter, r *http.Request) {
	status := "DISCONNECTED"
	if a.Config.SlackToken != "" {
		status = "CONNECTED"
	}
	logger.Debugf("[CHANNEL] Slack token status: %s", status)
	respondJSON(w, http.StatusOK, map[string]string{"status": status})
}
