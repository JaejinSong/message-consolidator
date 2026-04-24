package scanner

import (
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

// EnrichTelegramMessage normalizes Telegram raw data into the unified EnrichedMessage model.
// chatKey is the scanner-facing identifier produced by channels.peerKey (e.g. "tg_channel_123").
func EnrichTelegramMessage(chatKey string, msg string, timestamp time.Time) (*types.EnrichedMessage, error) {
	return enrichChannelMessage("telegram", "tg", chatKey, msg, timestamp, resolveTelegramSender), nil
}

// resolveTelegramSender maps the chat key (for DMs) to a known user. Group/channel
// chats fall back to the chat key — per-message sender resolution happens in the
// payload builder because one chat groups many senders.
func resolveTelegramSender(chatKey string) (int, string) {
	u, err := store.GetUserByTgID(chatKey)
	if err != nil {
		logger.Debugf("Telegram sender mapping failed for chatKey: %s. Using fallback.", chatKey)
		return int(0), chatKey
	}
	return u.ID, u.Name
}
