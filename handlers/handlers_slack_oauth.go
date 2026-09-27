package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"strings"
	"sync"
	"time"
)

// exchangeSlackUserCodeFunc / verifySlackUserEmailFunc are package-level seams so tests
// can fake the Slack API round trips without a live workspace.
var (
	exchangeSlackUserCodeFunc = channels.ExchangeSlackUserCode
	verifySlackUserEmailFunc  = channels.VerifySlackUserEmail
	revokeSlackUserTokenFunc  = channels.RevokeSlackUserToken
)

// slackOAuthNonceTTL bounds how long an issued CSRF nonce stays redeemable.
// Why: prime interval (13m) comfortably covers a slow OAuth consent screen without
// leaving stale nonces valid indefinitely.
const slackOAuthNonceTTL = 13 * time.Minute

type slackOAuthNonceEntry struct {
	email  string
	expiry time.Time
}

var (
	slackOAuthNonceMu sync.Mutex
	slackOAuthNonces  = make(map[string]slackOAuthNonceEntry)
)

// issueSlackOAuthNonce mints a one-time, unguessable CSRF token bound to email and
// performs lazy cleanup of expired entries. Why: a guessable "slack:"+email state would
// let anyone drive another user's callback; a random nonce keyed server-side prevents that.
func issueSlackOAuthNonce(email string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(b)

	slackOAuthNonceMu.Lock()
	defer slackOAuthNonceMu.Unlock()
	now := time.Now()
	for k, v := range slackOAuthNonces {
		if now.After(v.expiry) {
			delete(slackOAuthNonces, k)
		}
	}
	slackOAuthNonces[nonce] = slackOAuthNonceEntry{email: email, expiry: now.Add(slackOAuthNonceTTL)}
	return nonce, nil
}

// consumeSlackOAuthNonce redeems a nonce exactly once, rejecting unknown or expired ones.
func consumeSlackOAuthNonce(nonce string) (string, bool) {
	slackOAuthNonceMu.Lock()
	defer slackOAuthNonceMu.Unlock()
	entry, ok := slackOAuthNonces[nonce]
	if !ok {
		return "", false
	}
	delete(slackOAuthNonces, nonce)
	if time.Now().After(entry.expiry) {
		return "", false
	}
	return entry.email, true
}

// HandleSlackConnect starts the per-user Slack OAuth flow, mirroring HandleGmailConnect.
func (a *API) HandleSlackConnect(w http.ResponseWriter, r *http.Request) {
	if !channels.SlackUserOAuthConfigured() {
		respondError(w, http.StatusServiceUnavailable, "Slack OAuth is not configured")
		return
	}

	email := auth.GetUserEmail(r)
	nonce, err := issueSlackOAuthNonce(email)
	if err != nil {
		handleAPIError(w, r, err, "[SLACK-OAUTH]", "Failed to start Slack connect")
		return
	}

	url := channels.SlackUserAuthURL(nonce)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// HandleSlackCallback completes the per-user Slack OAuth flow. Unprotected like the Gmail
// callback since Slack redirects here without the app's own session cookie.
func (a *API) HandleSlackCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if reason := r.URL.Query().Get("error"); reason != "" {
		logger.Warnf("[SLACK-OAUTH] callback: user denied consent (%s)", reason)
		http.Redirect(w, r, "/?slack=denied", http.StatusTemporaryRedirect)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	email, ok := consumeSlackOAuthNonce(state)
	if !ok {
		respondError(w, http.StatusBadRequest, "Invalid or expired state")
		return
	}

	exch, err := exchangeSlackUserCodeFunc(ctx, code)
	if err != nil {
		logger.Warnf("[SLACK-OAUTH] callback: token exchange failed for %s: %v", email, err)
		respondError(w, http.StatusInternalServerError, "Token exchange failed")
		return
	}

	profileEmail, err := verifySlackUserEmailFunc(ctx, exch.AccessToken, exch.SlackUserID)
	if err != nil {
		logger.Warnf("[SLACK-OAUTH] callback: users.info verify failed for %s: %v", email, err)
		respondError(w, http.StatusInternalServerError, "Failed to verify Slack account")
		return
	}

	// Why: without this check the flow would let a user bind someone else's Slack account
	// (e.g. paste another person's consent redirect) to their own app session.
	if !strings.EqualFold(profileEmail, email) {
		logger.Warnf("[SLACK-OAUTH] callback: Slack profile email does not match app account for %s", email)
		http.Redirect(w, r, "/?slack=mismatch", http.StatusTemporaryRedirect)
		return
	}

	if err := saveSlackUserTokenFunc(ctx, email, store.SlackUserToken{
		Token:       exch.AccessToken,
		SlackUserID: exch.SlackUserID,
		Scopes:      exch.Scope,
	}); err != nil {
		logger.Warnf("[SLACK-OAUTH] callback: failed to save token for %s: %v", email, err)
		respondError(w, http.StatusInternalServerError, "Failed to save Slack token")
		return
	}

	logger.Infof("[SLACK-OAUTH] connected for %s", email)
	http.Redirect(w, r, "/?slack=connected", http.StatusTemporaryRedirect)
}

// saveSlackUserTokenFunc is a seam so tests can assert what gets saved without a real DB
// round trip when only the handler's control flow is under test.
var saveSlackUserTokenFunc = store.SaveSlackUserToken

// HandleSlackUserDisconnect removes the caller's own per-user Slack token.
func (a *API) HandleSlackUserDisconnect(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	// Why: best-effort — a failed revoke must not keep the user connected locally.
	if tok, ok, err := store.GetSlackUserToken(r.Context(), email); err == nil && ok {
		if revokeErr := revokeSlackUserTokenFunc(r.Context(), tok.Token); revokeErr != nil {
			logger.Warnf("[SLACK-OAUTH] revoke failed for %s: %v", email, revokeErr)
		}
	}
	if err := store.DeleteSlackUserToken(r.Context(), email); err != nil {
		handleAPIError(w, r, err, "[SLACK-OAUTH]", "Failed to disconnect Slack")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}
