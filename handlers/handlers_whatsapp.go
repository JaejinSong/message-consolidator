package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"net/http"
)

//Why: Returns the current WhatsApp connection status for the authenticated user, allowing the frontend to display the appropriate connection state.
func (a *API) HandleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	status := channels.GetWhatsAppStatus(email)
	logger.Debugf("[CHANNEL] WhatsApp status for %s: %s", email, status)
	respondJSON(w, http.StatusOK, map[string]string{"status": status})
}

//Why: Generates a base64-encoded QR code for WhatsApp authentication, which the user can scan to link their account to the service.
func (a *API) HandleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	qr, err := channels.GetWhatsAppQR(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"qr": qr})
}

//Why: Terminates the WhatsApp session for the authenticated user, effectively logging them out and revoking access tokens.
func (a *API) HandleWhatsAppLogout(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	err := channels.LogoutWhatsApp(email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}
