package scanner

import (
	"fmt"
	"strings"
	"sync"
	"time"

	waTypes "go.mau.fi/whatsmeow/types"

	"context"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

// whatsAppAdapter adapts WAManager to the shared channel scanner driver.
type whatsAppAdapter struct {
	// ctx is the scan-scoped context; BuildPayload resolves contact names through
	// store.GetNameByWhatsAppNumber and the ChannelAdapter interface carries no ctx
	// (mirrors slackAdapter's ctx field for the same reason).
	ctx context.Context
}

func (whatsAppAdapter) Source() string    { return store.SourceWhatsApp }
func (whatsAppAdapter) LogPrefix() string { return "WA" }
func (whatsAppAdapter) PopMessages(ctx context.Context, email string) map[string][]types.RawMessage {
	buffer := channels.DefaultWAManager.PopMessages(email)
	if len(buffer) == 0 {
		return buffer
	}
	var ids []string
	for _, msgs := range buffer {
		for _, m := range msgs {
			ids = append(ids, m.ID)
		}
	}
	if err := store.MarkWAMessagesPopped(ctx, email, ids); err != nil {
		logger.Warnf("[SCAN] WA: mark popped failed: %v", err)
	}
	return buffer
}

// AckScanned reconciles a scanned group's durable wa_messages rows: ok marks them
// processed so they never replay. !ok is a no-op here -- PopMessages already bumped
// scan_attempts for these ids, so leaving them alone (rather than double-counting) lets
// them stay eligible for the next replay pass, up to the retry cap.
func (whatsAppAdapter) AckScanned(ctx context.Context, email string, ids []string, ok bool) {
	if !ok {
		logger.Debugf("[SCAN] WA: scan failed for %d ids, leaving for replay", len(ids))
		return
	}
	if err := store.MarkWAMessagesProcessed(ctx, email, ids); err != nil {
		logger.Warnf("[SCAN] WA: ack scanned (ok=%v) failed: %v", ok, err)
	}
}
func (whatsAppAdapter) GetGroupName(email, roomKey string) string {
	return channels.DefaultWAManager.GetGroupName(email, roomKey)
}

// LegacyRoomName is the label WhatsApp history was written under before contact and group
// naming: the JID's user part, i.e. an opaque numeric id for an @lid chat and a bare phone
// number for a regular DM.
func (whatsAppAdapter) LegacyRoomName(roomKey string) string {
	jid, err := waTypes.ParseJID(roomKey)
	if err != nil {
		return ""
	}
	return jid.User
}

// Is1To1 — WhatsApp group JIDs carry the "@g.us" suffix; everything else is a DM.
func (whatsAppAdapter) Is1To1(roomKey string) bool { return !strings.Contains(roomKey, "@g.us") }

func (a whatsAppAdapter) BuildPayload(user store.User, aliases []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	return buildWAPayload(a.ctx, user, aliases, msgs)
}

func (whatsAppAdapter) Enrich(roomKey, payload string, ts time.Time) (*types.EnrichedMessage, error) {
	return EnrichWhatsAppMessage(roomKey, payload, ts)
}

func (whatsAppAdapter) IsFromMe(m types.RawMessage, user store.User) bool { return isFromMe(m, user) }

// SaveThreadID — quote replies anchor on the quoted stanza ID, root messages on their own ID.
func (whatsAppAdapter) SaveThreadID(m types.RawMessage) string {
	if m.ReplyToID != "" {
		return m.ReplyToID
	}
	return m.ID
}

// Mentions — WA pre-resolved display names power pickFirstMentionAssignee.
func (whatsAppAdapter) Mentions(m types.RawMessage) []string { return m.MentionedNames }

func buildWAPayload(ctx context.Context, user store.User, aliases []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	_ = aliases
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range msgs {
		msgMap[m.ID] = m
		resolvedText := channels.ResolveWAMentions(ctx, user.Email, m.Text, m.MentionedIDs)
		metaStr := buildWAMetadataString(ctx, user.Email, m)

		senderName := m.Sender
		if m.IsFromMe {
			senderName = user.Name
		} else if name := store.GetNameByWhatsAppNumber(ctx, user.Email, m.Sender); name != "" {
			senderName = name
		}

		tsTag := formatTSTag(m.Timestamp)
		writePayloadLine(&sb, m.ID, tsTag, metaStr, senderName, resolvedText)
	}
	return sb.String(), msgMap
}

func buildWAMetadataString(ctx context.Context, email string, m types.RawMessage) string {
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
		tags = append(tags, formatWAMentionTag(ctx, email, m.MentionedIDs))
	}

	return formatTagBlock(tags) + formatListBlock("Files", m.AttachmentNames)
}

// Why: Splits the mention-tag formatting out of buildWAMetadataString so the parent function avoids deep nesting and stays in nestif budget.
func formatWAMentionTag(ctx context.Context, email string, mentionedIDs []string) string {
	var names []string
	for _, jid := range mentionedIDs {
		id, _ := waTypes.ParseJID(jid)
		if id.User == "" {
			continue
		}
		if name := store.GetNameByWhatsAppNumber(ctx, email, id.User); name != "" {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		return fmt.Sprintf("Explicit-Mentions: %s", strings.Join(names, ", "))
	}
	return fmt.Sprintf("Mentions: %d", len(mentionedIDs))
}

func scanWhatsApp(ctx context.Context, user store.User, aliases []string, language string, wg *sync.WaitGroup) []store.MessageID {
	return scanChannel(ctx, user, aliases, language, wg, whatsAppAdapter{ctx: ctx})
}
