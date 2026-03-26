package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"net/http"
)

func HandleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	status := channels.GetWhatsAppStatus(email)
	logger.Debugf("[CHANNEL] WhatsApp status for %s: %s", email, status)
	respondJSON(w, map[string]string{"status": status})
}

func HandleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	qr, err := channels.GetWhatsAppQR(r.Context(), email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]string{"qr": qr})
}

func HandleWhatsAppLogout(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	err := channels.LogoutWhatsApp(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]string{"status": "logged_out"})
}
