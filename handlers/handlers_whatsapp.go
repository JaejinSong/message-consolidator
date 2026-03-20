package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"net/http"
)

func HandleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	status := channels.GetWhatsAppStatus(email)
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
