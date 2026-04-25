package scanner

import (
	"fmt"
	"time"

	"message-consolidator/types"
)

// enrichChannelMessage normalizes channel raw data into the shared EnrichedMessage.
// threadPrefix is the short namespace ("wa", "tg", …) and resolveSender maps the
// roomKey to (senderID, senderName), returning (0, roomKey) as the documented fallback.
func enrichChannelMessage(source, threadPrefix, roomKey, payload string, ts time.Time, resolveSender func(string) (int64, string)) *types.EnrichedMessage {
	windowStart := calculateWindowStart(ts)
	senderID, senderName := resolveSender(roomKey)

	return &types.EnrichedMessage{
		RawContent:      payload,
		SourceChannel:   source,
		SenderID:        senderID,
		SenderName:      senderName,
		VirtualThreadID: fmt.Sprintf("%s_thread_%s_%d", threadPrefix, roomKey, windowStart),
		Timestamp:       ts,
	}
}

// calculateWindowStart truncates a timestamp to the start of a 15-minute (900s) window.
func calculateWindowStart(t time.Time) int64 {
	return (t.Unix() / 900) * 900
}
