package handlers

import (
	"encoding/json"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// gmailStaleThreshold marks the scan as stale when the last clean pass is older than
// this. Why: prime interval (31m) > the scan cycle so one slow cycle cannot flap the badge.
const gmailStaleThreshold = 31 * time.Minute

type gmailStatusResponse struct {
	Connected  bool  `json:"connected"`
	LastScanAt int64 `json:"last_scan_at"`
	Stale      bool  `json:"stale"`
}

// buildGmailStatus derives the status payload from token presence and the last_success
// scan stamp. lastSuccessTS="" (never scanned, e.g. right after first connect) is not stale.
func buildGmailStatus(connected bool, lastSuccessTS string, now time.Time) gmailStatusResponse {
	resp := gmailStatusResponse{Connected: connected}
	ts, err := strconv.ParseInt(lastSuccessTS, 10, 64)
	if err != nil || ts <= 0 {
		return resp
	}
	resp.LastScanAt = ts
	resp.Stale = connected && now.Sub(time.Unix(ts, 0)) > gmailStaleThreshold
	return resp
}

func (a *API) HandleGmailConnect(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	state := "gmail:" + email
	url := channels.GetGmailAuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect) //nolint:gosec // G710: host comes from the oauth2 config's fixed Google endpoint; only state is caller-derived
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

	tokenJSON, err := json.Marshal(token) //nolint:gosec // G117: token JSON is persisted server-side, never sent to a client
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
	lastSuccess := store.GetLastScan(email, store.SourceGmail, store.ScanTargetLastSuccess)
	resp := buildGmailStatus(connected, lastSuccess, time.Now())
	logger.Debugf("[GMAIL] status for %s: connected=%v stale=%v last_scan_at=%d", email, connected, resp.Stale, resp.LastScanAt)
	w.Header().Set("Content-Type", "application/json")
	respondJSON(w, http.StatusOK, resp)
}

func (a *API) HandleGmailDisconnect(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	if err := store.DeleteGmailToken(r.Context(), email); err != nil {
		handleAPIError(w, r, err, "[GMAIL]", "Failed to disconnect Gmail")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}
