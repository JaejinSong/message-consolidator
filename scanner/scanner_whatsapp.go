package scanner

import (
	"fmt"
	"strings"
	"sync"
	"time"

	waTypes "go.mau.fi/whatsmeow/types"

	"context"
	"message-consolidator/channels"
	"message-consolidator/store"
	"message-consolidator/types"
)

// whatsAppAdapter adapts WAManager to the shared channel scanner driver.
type whatsAppAdapter struct{}

func (whatsAppAdapter) Source() string    { return "whatsapp" }
func (whatsAppAdapter) LogPrefix() string { return "WA" }
func (whatsAppAdapter) PopMessages(email string) map[string][]types.RawMessage {
	return channels.DefaultWAManager.PopMessages(email)
}
func (whatsAppAdapter) GetGroupName(email, roomKey string) string {
	return channels.DefaultWAManager.GetGroupName(email, roomKey)
}

// Is1To1 — WhatsApp group JIDs carry the "@g.us" suffix; everything else is a DM.
func (whatsAppAdapter) Is1To1(roomKey string) bool { return !strings.Contains(roomKey, "@g.us") }

func (whatsAppAdapter) BuildPayload(user store.User, aliases []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	return buildWAPayload(user, aliases, msgs)
}

func (whatsAppAdapter) Enrich(roomKey, payload string, ts time.Time) (*types.EnrichedMessage, error) {
	return EnrichWhatsAppMessage(roomKey, payload, ts)
}

func buildWAPayload(user store.User, aliases []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	_ = aliases
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range msgs {
		msgMap[m.ID] = m
		resolvedText := channels.ResolveWAMentions(user.Email, m.Text, m.MentionedIDs)
		metaStr := buildWAMetadataString(user.Email, m)

		senderName := m.Sender
		if m.IsFromMe {
			senderName = user.Name
		} else if name := store.GetNameByWhatsAppNumber(user.Email, m.Sender); name != "" {
			senderName = name
		}

		sb.WriteString(fmt.Sprintf("[ID:%s]%s %s: %s\n", m.ID, metaStr, senderName, resolvedText))
	}
	return sb.String(), msgMap
}

func buildWAMetadataString(email string, m types.RawMessage) string {
	var tags []string
	if m.IsForwarded {
		tags = append(tags, "Forwarded")
	}
	if m.RepliedToUser != "" {
		tags = append(tags, fmt.Sprintf("Reply-To: %s", m.RepliedToUser))
	}

	// Why: Lists explicitly mentioned names in metadata to give the AI a 100% accurate
	// source for 'Assignee' identification; falls back to a bare count when unresolved.
	if len(m.MentionedIDs) > 0 {
		var mentionNames []string
		for _, jid := range m.MentionedIDs {
			if id, _ := waTypes.ParseJID(jid); id.User != "" {
				if name := store.GetNameByWhatsAppNumber(email, id.User); name != "" {
					mentionNames = append(mentionNames, name)
				}
			}
		}
		if len(mentionNames) > 0 {
			tags = append(tags, fmt.Sprintf("Explicit-Mentions: %s", strings.Join(mentionNames, ", ")))
		} else {
			tags = append(tags, fmt.Sprintf("Mentions: %d", len(m.MentionedIDs)))
		}
	}

	var sb strings.Builder
	if len(tags) > 0 {
		sb.WriteString(fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", ")))
	}
	if len(m.AttachmentNames) > 0 {
		sb.WriteString(fmt.Sprintf(" [Files: %s]", strings.Join(m.AttachmentNames, ", ")))
	}
	return sb.String()
}

func scanWhatsApp(ctx context.Context, user store.User, aliases []string, language string, wg *sync.WaitGroup) []int {
	return scanChannel(ctx, user, aliases, language, wg, whatsAppAdapter{})
}
