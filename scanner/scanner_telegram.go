package scanner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"message-consolidator/channels"
	"message-consolidator/store"
	"message-consolidator/types"
)

// telegramAdapter adapts TelegramManager to the shared channel scanner driver.
type telegramAdapter struct{}

func (telegramAdapter) Source() string    { return "telegram" }
func (telegramAdapter) LogPrefix() string { return "TG" }
func (telegramAdapter) PopMessages(email string) map[string][]types.RawMessage {
	return channels.DefaultTelegramManager.PopMessages(email)
}
func (telegramAdapter) GetGroupName(email, roomKey string) string {
	return channels.DefaultTelegramManager.GetGroupName(email, roomKey)
}

// Is1To1 — Telegram peerKey prefixes distinguish DM ("tg_user_") from group/channel.
func (telegramAdapter) Is1To1(roomKey string) bool { return strings.HasPrefix(roomKey, "tg_user_") }

func (telegramAdapter) BuildPayload(user store.User, _ []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	return buildTGPayload(user, msgs)
}

func (telegramAdapter) Enrich(roomKey, payload string, ts time.Time) (*types.EnrichedMessage, error) {
	return EnrichTelegramMessage(roomKey, payload, ts)
}

func buildTGPayload(user store.User, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range msgs {
		msgMap[m.ID] = m
		meta := buildTGMetadataString(m)

		senderName := m.SenderName
		if senderName == "" {
			senderName = m.Sender
		}
		if m.IsFromMe {
			senderName = user.Name
		}

		sb.WriteString(fmt.Sprintf("[ID:%s]%s %s: %s\n", m.ID, meta, senderName, m.Text))
	}
	return sb.String(), msgMap
}

func buildTGMetadataString(m types.RawMessage) string {
	var tags []string
	if m.IsForwarded {
		tags = append(tags, "Forwarded")
	}
	if m.RepliedToUser != "" {
		tags = append(tags, fmt.Sprintf("Reply-To: %s", m.RepliedToUser))
	}

	var sb strings.Builder
	if len(tags) > 0 {
		sb.WriteString(fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", ")))
	}
	if m.HasAttachment {
		sb.WriteString(" [HasAttachment: true]")
	}
	return sb.String()
}

func scanTelegram(ctx context.Context, user store.User, aliases []string, language string, wg *sync.WaitGroup) []store.MessageID {
	return scanChannel(ctx, user, aliases, language, wg, telegramAdapter{})
}
