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

	// Ensure the callback originates from a Gmail auth flow to prevent CSRF or misrouted callbacks
	if !strings.HasPrefix(state, "gmail:") {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	email := strings.TrimPrefix(state, "gmail:")

	token, err := channels.ExchangeGmailCode(ctx, code)
	if err != nil {
		logger.Debugf("[GMAIL-CALLBACK] Token exchange failed for %s: %v", email, err)
		http.Error(w, "Token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		http.Error(w, "Failed to marshal token", http.StatusInternalServerError)
		return
	}

	if err := store.SaveGmailToken(email, string(tokenJSON)); err != nil {
		logger.Debugf("[GMAIL-CALLBACK] Failed to save token for %s: %v", email, err)
		http.Error(w, "Failed to save token", http.StatusInternalServerError)
		return
	}

	logger.Infof("[GMAIL-CALLBACK] Gmail connected for %s", email)
	http.Redirect(w, r, "/?gmail=connected", http.StatusTemporaryRedirect)
}

func (a *API) HandleGmailStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	connected := store.HasGmailToken(email)
	logger.Debugf("[CHANNEL] Gmail status for %s: connected=%v", email, connected)
	w.Header().Set("Content-Type", "application/json")
	respondJSON(w, http.StatusOK, map[string]bool{"connected": connected})
}

func (a *API) HandleGmailDisconnect(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	if err := store.DeleteGmailToken(email); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}
