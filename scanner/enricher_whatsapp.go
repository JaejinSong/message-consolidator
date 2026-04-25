package scanner

import (
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

// EnrichWhatsAppMessage normalizes WhatsApp raw data into a unified EnrichedMessage model.
func EnrichWhatsAppMessage(rawJID string, msg string, timestamp time.Time) (*types.EnrichedMessage, error) {
	return enrichChannelMessage("whatsapp", "wa", rawJID, msg, timestamp, whatsAppSenderShim), nil
}

// resolveWhatsAppSender maps a WhatsApp JID to a system user, falling back to raw JID if unmapped.
func resolveWhatsAppSender(rawJID string) (store.UserID, string) {
	u, err := store.GetUserByWAJID(rawJID)
	if err != nil {
		logger.Debugf("WhatsApp sender mapping failed for JID: %s. Using fallback.", rawJID)
		return 0, rawJID
	}
	return u.ID, u.Name
}

// whatsAppSenderShim adapts the phantom-typed UserID return to the int64 boundary
// expected by enrichChannelMessage (types package cannot import store).
func whatsAppSenderShim(rawJID string) (int64, string) {
	id, name := resolveWhatsAppSender(rawJID)
	return int64(id), name
}
