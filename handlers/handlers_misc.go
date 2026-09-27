package handlers

import (
	"fmt"
	"message-consolidator/auth"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
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

	data, err := os.ReadFile(fileName) //nolint:gosec // G703: noteType is allowlisted and lang is isAlpha-validated, so no traversal sequence is representable
	if os.IsNotExist(err) {
		// Fallback to English if the requested language is not found
		logger.Warnf("[RELEASE] lang '%s' not found, falling back to EN.", lang)
		fallbackFileName := fmt.Sprintf("./RELEASE_NOTES_%s_EN.md", noteType)
		data, err = os.ReadFile(fallbackFileName) //nolint:gosec // G703: fallback path interpolates only the allowlisted noteType
	}

	if err != nil {
		logger.Errorf("[RELEASE] read failed %s (or its fallback): %v", fileName, err)
		respondError(w, http.StatusInternalServerError, "Failed to load release notes")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"content": string(data)})
}

type slackStatusResponse struct {
	Status           string `json:"status"`
	SlackID          string `json:"slack_id,omitempty"`
	LastScanAt       int64  `json:"last_scan_at"`
	Stale            bool   `json:"stale"`
	UserToken        bool   `json:"user_token"`
	UserTokenSlackID string `json:"user_token_slack_id,omitempty"`
}

// slackStaleThreshold marks the scan as stale when the last clean pass is older than
// this. Why: mirrors gmailStaleThreshold — prime interval (31m) > the scan cycle so one
// slow cycle cannot flap the badge.
const slackStaleThreshold = 31 * time.Minute

// buildSlackStatus derives the status payload from token presence and the last_success
// scan stamp, mirroring buildGmailStatus. lastSuccessTS="" (never scanned, e.g. right
// after first connect) is not stale. hasUserToken/userTokenSlackID report the caller's own
// per-user Slack OAuth link, independent of the workspace bot token.
func buildSlackStatus(connected bool, slackID, lastSuccessTS string, now time.Time, hasUserToken bool, userTokenSlackID string) slackStatusResponse {
	resp := slackStatusResponse{SlackID: slackID, UserToken: hasUserToken, UserTokenSlackID: userTokenSlackID}
	if connected {
		resp.Status = "connected"
	} else {
		resp.Status = "disconnected"
	}
	ts, err := strconv.ParseInt(lastSuccessTS, 10, 64)
	if err != nil || ts <= 0 {
		return resp
	}
	resp.LastScanAt = ts
	resp.Stale = connected && now.Sub(time.Unix(ts, 0)) > slackStaleThreshold
	return resp
}

// Why: Checks the presence of the Slack API token to determine the current connection status of the Slack integration.
// Also returns the caller's mapped slack_id so the Connections UI can show what account
// the workspace bot has linked to this user, plus scan freshness (last_scan_at/stale) —
// token presence alone stayed "connected" through the 2026-09-17 incident where the bot
// was removed from every channel and scanning silently stopped for 10 days.
//
// Status string convention: lowercase — "connected" / "disconnected".
// All channel status handlers (whatsapp, telegram, slack) emit lowercase so the frontend
// can compare via a single helper (isStatusConnected) without per-channel casing exceptions.
func (a *API) HandleSlackStatus(w http.ResponseWriter, r *http.Request) {
	connected := a.Config.SlackToken != ""

	email := auth.GetUserEmail(r)
	slackID := ""
	if email != "" {
		if user, err := store.GetOrCreateUser(r.Context(), email, "", ""); err == nil && user != nil {
			slackID = user.SlackID
		}
	}

	lastSuccess := store.GetLastScan(email, store.SourceSlack, store.ScanTargetLastSuccess)
	// Why: HasSlackUserToken is cache-only; gating the GetSlackUserToken call behind it
	// avoids a DB round trip (and touching the DB at all) when nothing is cached.
	hasUserToken := store.HasSlackUserToken(email)
	userTokenSlackID := ""
	if hasUserToken {
		if userToken, ok, err := store.GetSlackUserToken(r.Context(), email); err == nil && ok {
			userTokenSlackID = userToken.SlackUserID
		}
	}
	resp := buildSlackStatus(connected, slackID, lastSuccess, time.Now(), hasUserToken, userTokenSlackID)

	logger.Debugf("[SLACK] status for %s: %s (slackID=%q stale=%v last_scan_at=%d)", email, resp.Status, resp.SlackID, resp.Stale, resp.LastScanAt)
	respondJSON(w, http.StatusOK, resp)
}
