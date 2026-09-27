package scanner

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"message-consolidator/channels"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

// waReplayPool — replay runs roughly every ~30 minutes, deliberately longer than
// waCreatedGrace (13 min) so a live scan tick always gets first crack at a fresh
// message before replay ever considers it. All entries prime per project convention.
var waReplayPool = []time.Duration{
	1789 * time.Second,
	1801 * time.Second,
	1811 * time.Second,
}

// runWhatsAppReplay requeues wa_messages rows a scan never finished (crashed mid-scan,
// AI outage with empty fallback, enrichment failure) back into the live ChatBuffer so
// the next WhatsApp scan tick processes them exactly like a freshly received message.
func runWhatsAppReplay(ctx context.Context, _ *sync.WaitGroup) {
	defer safego.Recover("wa-replay")
	bundles := loadUsersForScan(ctx)
	for _, b := range bundles {
		replayWhatsAppUser(ctx, b.user.Email)
	}
}

func replayWhatsAppUser(ctx context.Context, email string) {
	rows, err := store.ListReplayableWAMessages(ctx, email, time.Now())
	if err != nil {
		logger.Warnf("[WA-REPLAY] %s: list replayable failed: %v", email, err)
		return
	}
	if len(rows) == 0 {
		return
	}

	byChat, malformed := groupReplayableByChat(rows)
	if len(malformed) > 0 {
		if err := store.MarkWAMessagesFailed(ctx, email, malformed); err != nil {
			logger.Warnf("[WA-REPLAY] %s: mark malformed failed failed: %v", email, err)
		}
	}

	requeued := 0
	for chatJID, raws := range byChat {
		requeued += channels.DefaultWAManager.ReplayMessages(email, chatJID, raws)
	}
	if requeued > 0 {
		logger.Infof("[WA-REPLAY] %s: requeued %d message(s) across %d chat(s) (malformed=%d)",
			email, requeued, len(byChat), len(malformed))
	}
}

// groupReplayableByChat unmarshals each row's raw_json into types.RawMessage and groups
// them by chat JID. Rows whose raw_json fails to parse are returned separately (by
// message ID) so the caller can mark them failed and let the retry cap age them out.
func groupReplayableByChat(rows []store.ReplayableWAMessage) (map[string][]types.RawMessage, []string) {
	byChat := make(map[string][]types.RawMessage)
	var malformed []string
	for _, row := range rows {
		var raw types.RawMessage
		if err := json.Unmarshal([]byte(row.RawJSON), &raw); err != nil {
			logger.Warnf("[WA-REPLAY] malformed raw_json for message %s: %v", row.MessageID, err)
			malformed = append(malformed, row.MessageID)
			continue
		}
		byChat[row.ChatJID] = append(byChat[row.ChatJID], raw)
	}
	return byChat, malformed
}
