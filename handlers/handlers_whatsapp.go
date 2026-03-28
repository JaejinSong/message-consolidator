package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"net/http"
)

func (a *API) HandleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	status := channels.GetWhatsAppStatus(email)
	logger.Debugf("[CHANNEL] WhatsApp status for %s: %s", email, status)
	respondJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (a *API) HandleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	qr, err := channels.GetWhatsAppQR(r.Context(), email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"qr": qr})
}

func (a *API) HandleWhatsAppLogout(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	err := channels.LogoutWhatsApp(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}
