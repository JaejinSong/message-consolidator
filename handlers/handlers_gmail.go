package handlers

import (
	"encoding/json"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"strings"
)

func (a *API) HandleGmailConnect(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	state := "gmail:" + email
	url := channels.GetGmailAuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (a *API) HandleGmailCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	//Why: Verifies that the callback originates from a Gmail auth flow to prevent CSRF or misrouted authentication requests.
	if !strings.HasPrefix(state, "gmail:") {
		respondError(w, http.StatusBadRequest, "Invalid state")
		return
	}
	email := strings.TrimPrefix(state, "gmail:")

	token, err := channels.ExchangeGmailCode(ctx, code)
	if err != nil {
		logger.Warnf("[GMAIL] callback: token exchange failed for %s: %v", email, err)
		respondError(w, http.StatusInternalServerError, "Token exchange failed: "+err.Error())
		return
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		handleAPIError(w, r, err, "[GMAIL]", "Failed to marshal token")
		return
	}

	if err := store.SaveGmailToken(r.Context(), email, string(tokenJSON)); err != nil {
		logger.Warnf("[GMAIL] callback: failed to save token for %s: %v", email, err)
		respondError(w, http.StatusInternalServerError, "Failed to save token")
		return
	}

	logger.Infof("[GMAIL] connected for %s", email)
	http.Redirect(w, r, "/?gmail=connected", http.StatusTemporaryRedirect)
}

func (a *API) HandleGmailStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	connected := store.HasGmailToken(email)
	logger.Debugf("[GMAIL] status for %s: connected=%v", email, connected)
	w.Header().Set("Content-Type", "application/json")
	respondJSON(w, http.StatusOK, map[string]bool{"connected": connected})
}

func (a *API) HandleGmailDisconnect(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	if err := store.DeleteGmailToken(r.Context(), email); err != nil {
		handleAPIError(w, r, err, "[GMAIL]", "Failed to disconnect Gmail")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}
