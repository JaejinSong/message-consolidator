package scanner

import (
	"fmt"
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

// EnrichWhatsAppMessage normalizes WhatsApp raw data into a unified EnrichedMessage model.
// Why: Standardizes incoming WhatsApp messages with user identity lookup and virtual threading for consistent AI analysis.
func EnrichWhatsAppMessage(rawJID string, msg string, timestamp time.Time, aliasStore *store.AliasStore) (*types.EnrichedMessage, error) {
	windowStart := calculateWindowStart(timestamp)
	threadID := fmt.Sprintf("wa_thread_%s_%d", rawJID, windowStart)

	senderID, senderName := resolveWhatsAppSender(rawJID)

	enriched := &types.EnrichedMessage{
		RawContent:      msg,
		SourceChannel:   "whatsapp",
		SenderID:        senderID,
		SenderName:      senderName,
		VirtualThreadID: threadID,
		Timestamp:       timestamp,
	}

	return enriched, nil
}

// resolveWhatsAppSender maps a WhatsApp JID to a system user, falling back to raw JID if unmapped.
func resolveWhatsAppSender(rawJID string) (int, string) {
	u, err := store.GetUserByWAJID(rawJID)
	if err != nil {
		logger.Debugf("WhatsApp sender mapping failed for JID: %s. Using fallback.", rawJID)
		return int(0), rawJID // Why: Explicit integer conversion for identity fallback security.
	}

	return u.ID, u.Name
}

// calculateWindowStart truncates a timestamp to the start of a 15-minute (900s) window.
func calculateWindowStart(t time.Time) int64 {
	return (t.Unix() / 900) * 900
}
