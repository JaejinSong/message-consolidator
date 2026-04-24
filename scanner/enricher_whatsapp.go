package scanner

import (
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

// EnrichWhatsAppMessage normalizes WhatsApp raw data into a unified EnrichedMessage model.
func EnrichWhatsAppMessage(rawJID string, msg string, timestamp time.Time) (*types.EnrichedMessage, error) {
	return enrichChannelMessage("whatsapp", "wa", rawJID, msg, timestamp, resolveWhatsAppSender), nil
}

// resolveWhatsAppSender maps a WhatsApp JID to a system user, falling back to raw JID if unmapped.
func resolveWhatsAppSender(rawJID string) (int, string) {
	u, err := store.GetUserByWAJID(rawJID)
	if err != nil {
		logger.Debugf("WhatsApp sender mapping failed for JID: %s. Using fallback.", rawJID)
		return int(0), rawJID
	}
	return u.ID, u.Name
}
