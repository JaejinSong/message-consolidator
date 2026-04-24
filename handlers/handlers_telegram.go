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

// HandleTelegramStatus returns the current Telegram connection state plus whether the user
// has usable App ID/Hash credentials (so the UI can decide to show the setup step).
func (a *API) HandleTelegramStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	status := channels.GetTelegramStatus(email)
	hasCreds := channels.HasTelegramCredentials(email, a.Config)
	logger.Debugf("[CHANNEL] Telegram status for %s: %s (hasCreds=%v)", email, status, hasCreds)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":          status,
		"has_credentials": hasCreds,
	})
}

// HandleTelegramSetCredentials persists per-user App ID / Hash obtained from https://my.telegram.org.
// Body: {"app_id": 12345, "app_hash": "abcd..."}.
func (a *API) HandleTelegramSetCredentials(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var body struct {
		AppID   int    `json:"app_id"`
		AppHash string `json:"app_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.AppHash = strings.TrimSpace(body.AppHash)
	if body.AppID <= 0 || body.AppHash == "" {
		respondError(w, http.StatusBadRequest, "app_id (positive integer) and app_hash are required")
		return
	}
	if err := store.UpsertTelegramCreds(r.Context(), email, body.AppID, body.AppHash); err != nil {
		logger.Warnf("[CHANNEL] UpsertTelegramCreds failed for %s: %v", email, err)
		respondError(w, http.StatusInternalServerError, "failed to save credentials")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// HandleTelegramAuthStart begins the phone-number auth flow (step 1 of 3).
// Body: {"phone": "+821012345678"}
func (a *API) HandleTelegramAuthStart(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.Phone) == "" {
		respondError(w, http.StatusBadRequest, "phone required")
		return
	}
	if err := channels.StartTelegramAuth(email, body.Phone, a.Config); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "code_sent"})
}

// HandleTelegramAuthConfirm submits the OTP (step 2 of 3).
// Body: {"code": "12345"} → either "connected" or "password_required".
func (a *API) HandleTelegramAuthConfirm(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	needsPassword, err := channels.ConfirmTelegramCode(email, body.Code)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if needsPassword {
		respondJSON(w, http.StatusOK, map[string]string{"status": "password_required"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

// HandleTelegramAuthPassword submits the 2FA password (step 3 of 3, optional).
// Body: {"password": "..."}
func (a *API) HandleTelegramAuthPassword(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := channels.ConfirmTelegramPassword(email, body.Password); err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

// HandleTelegramLogout tears down the running client and deletes the stored session.
func (a *API) HandleTelegramLogout(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	if err := channels.LogoutTelegram(email); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}
