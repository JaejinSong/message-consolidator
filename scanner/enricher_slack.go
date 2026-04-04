package scanner

import (
	"fmt"
	"message-consolidator/types"
	"time"
)

// EnrichSlackMessage normalizes Slack raw data into a unified EnrichedMessage model.
// Why: Standardizes Slack message metadata with user identity resolution and thread tracking for multi-channel AI analysis.
func EnrichSlackMessage(userID, userName, channelID, threadTS string, msg string, timestamp time.Time) (*types.EnrichedMessage, error) {
	// Why: VirtualThreadID for Slack uses the thread_ts if available, otherwise falls back to channel_id.
	// This ensures AI considers the full context of a conversation thread.
	threadID := threadTS
	if threadID == "" {
		threadID = channelID
	}

	enriched := &types.EnrichedMessage{
		RawContent:      msg,
		SourceChannel:   "slack",
		SenderID:        0, // ID would require a DB lookup if needed, using 0 as default.
		SenderName:      userName,
		VirtualThreadID: fmt.Sprintf("slack_thread_%s", threadID),
		Timestamp:       timestamp,
	}

	return enriched, nil
}
