package handlers

import (
	"errors"
	"net/http"

	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"

	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
)

type lineStatusResponse struct {
	Status string `json:"status"`
}

// HandleLINEStatus returns the LINE bot connection status (connected / disconnected).
func (a *API) HandleLINEStatus(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, lineStatusResponse{Status: channels.GetLINEStatus()})
}

// HandleLINEWebhook is a public endpoint (no auth middleware) that receives events
// from the LINE Platform. It verifies the HMAC-SHA256 signature with the channel secret,
// persists text messages to line_inbox, and always responds 200 within the LINE 5s window.
func (a *API) HandleLINEWebhook(w http.ResponseWriter, r *http.Request) {
	// Why: public endpoint — cap body to 1 MiB to prevent memory exhaustion from malicious payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	secret := a.Config.LineChannelSecret
	if secret == "" {
		logger.Warnf("[LINE] webhook received but LINE_CHANNEL_SECRET not configured")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	params, err := channels.ParseLineWebhook(secret, r)
	if err != nil {
		if errors.Is(err, webhook.ErrInvalidSignature) {
			logger.Warnf("[LINE] webhook: invalid signature")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		logger.Errorf("[LINE] webhook parse error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	for _, p := range params {
		if p.SenderID != "" && p.SenderName == "" {
			p.SenderName = channels.DefaultLineManager.ResolveSenderName(p.SenderID)
		}
		if err := store.InsertLineInbox(r.Context(), p); err != nil {
			logger.Errorf("[LINE] InsertLineInbox error (msgID=%s): %v", p.LineMessageID, err)
		}
	}

	w.WriteHeader(http.StatusOK)
}
