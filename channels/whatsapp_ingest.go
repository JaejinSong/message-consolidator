package channels

import (
	"context"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// any 사유: whatsmeow 이벤트는 events.Message/events.Connected 등 다양한 구조체로 내려옴 — type switch로 디스패치.
func (m *WAManager) handleEvent(email string, client *whatsmeow.Client, evt any) {
	switch v := evt.(type) {
	case *events.Message:
		m.handleMessageEvent(email, client, v)
	case *events.Picture:
		// [Optimization] 프로필 사진 업데이트 이벤트 무시
		return
	case *events.Connected:
		logger.Debugf("[WA] event for %s: connected to WhatsApp", email)
		if client.Store.ID != nil {
			m.OnConnected(email, client.Store.ID.String())
		}
	case *events.OfflineSyncCompleted:
		logger.Debugf("[WA] event for %s: offline sync completed", email)
	case *events.LoggedOut:
		logger.Debugf("[WA] event for %s: logged out", email)
		m.OnLoggedOut(email)
		m.mu.Lock()
		delete(m.clients, email)
		delete(m.latestQR, email)
		m.mu.Unlock()
		m.chatBuf.drop(email)
	default:
	}
}

// isSystemMessage는 해당 메시지가 사용자 발화가 아닌 프로토콜/시스템 알림인지 판별합니다.
func isSystemMessage(msg *events.Message) bool {
	if msg == nil || msg.Message == nil {
		return true
	}
	// Why: [Protocol Filter] ProtocolMessage는 본문 없는 제어용 메시지(삭제, 동기화 등)이므로 제외합니다.
	if msg.Message.ProtocolMessage != nil || msg.Message.SenderKeyDistributionMessage != nil {
		return true
	}
	// Why: [Status/System Filter] 'status@broadcast' 발신자 또는 PushName이 없는 시스템 알림을 차단합니다.
	if msg.Info.Sender.User == "status" || (msg.Info.PushName == "" && !msg.Info.IsFromMe && !msg.Info.IsGroup) {
		return true
	}
	// Why: [Category Filter] Category가 'peer'인 메시지는 디바이스 간 통신용이므로 제외합니다.
	if msg.Info.Category == "peer" {
		return true
	}
	return false
}

// Why: Separates complex message handling logic from the main event router to improve readability and maintainability.
func (m *WAManager) handleMessageEvent(email string, client *whatsmeow.Client, msg *events.Message) {
	if isSystemMessage(msg) {
		return
	}

	msgText, meta, ok := m.parseMessageContent(email, client, msg)
	if !ok || msgText == "" {
		return
	}

	sender := m.resolveSenderName(email, client, msg.Info)
	msgText = m.resolveIncomingMentions(email, client, msgText, meta.MentionedIDs)
	mentionedNames := m.resolveIncomingMentionNames(email, client, meta.MentionedIDs)

	raw := types.RawMessage{
		ID: msg.Info.ID, Sender: sender, Text: msgText,
		Timestamp: msg.Info.Timestamp, ReplyToID: meta.ReplyToID,
		RepliedToUser: meta.RepliedToUser, IsForwarded: meta.IsForwarded,
		IsFromMe:        msg.Info.IsFromMe,
		MentionedIDs:    meta.MentionedIDs, HasAttachment: meta.HasAttachment,
		AttachmentNames: meta.AttachmentNames,
		MentionedNames:  mentionedNames,
	}
	m.bufferMessage(email, msg.Info.Chat, raw)
	if m.OnMessage != nil {
		m.OnMessage(email, msg.Info.Chat.String(), raw)
	}

	logger.Debugf("[WA] event for %s: message from %s (chat: %s): %s", email, sender, msg.Info.Chat, msgText)
}

type messageMetadata struct {
	ReplyToID       string
	RepliedToUser   string
	IsForwarded     bool
	MentionedIDs    []string
	HasAttachment   bool
	AttachmentNames []string
}

func (m *WAManager) parseMessageContent(email string, client *whatsmeow.Client, msg *events.Message) (string, messageMetadata, bool) {
	var meta messageMetadata
	var text string

	if conv := msg.Message.GetConversation(); conv != "" {
		return conv, meta, true
	}

	if ext := msg.Message.GetExtendedTextMessage(); ext != nil {
		text = ext.GetText()
		meta.IsForwarded = ext.ContextInfo.GetIsForwarded()
		meta.MentionedIDs = ext.ContextInfo.MentionedJID
		if ext.ContextInfo != nil {
			meta.ReplyToID = ext.ContextInfo.GetStanzaID()
			meta.RepliedToUser = m.resolveRepliedUser(email, client, ext.ContextInfo)
		}
		return text, meta, true
	}

	text, meta.HasAttachment, meta.AttachmentNames = m.extractMediaInfo(msg.Message)
	return text, meta, text != ""
}

func (m *WAManager) resolveSenderName(email string, client *whatsmeow.Client, info waTypes.MessageInfo) string {
	if info.IsFromMe {
		return email
	}
	if info.PushName != "" {
		go func(em, num, name string) {
			defer safego.Recover("wa-save-contact")
			if err := store.SaveWhatsAppContact(context.Background(), em, num, name); err != nil {
				logger.Warnf("[WA] SaveWhatsAppContact failed for %s/%s: %v", em, num, err)
			}
		}(email, info.Sender.User, info.PushName)
		return info.PushName
	}
	return info.Sender.String()
}

func (m *WAManager) resolveRepliedUser(email string, client *whatsmeow.Client, ctx *waProto.ContextInfo) string {
	if ctx == nil || ctx.Participant == nil {
		return ""
	}
	repliedJID, _ := waTypes.ParseJID(*ctx.Participant)
	if name := store.GetNameByWhatsAppNumber(email, repliedJID.User); name != "" {
		return name
	}
	if contact, err := client.Store.Contacts.GetContact(context.Background(), repliedJID); err == nil {
		if contact.FullName != "" {
			return contact.FullName
		}
		return contact.PushName
	}
	return repliedJID.User
}

func (m *WAManager) bufferMessage(email string, chat waTypes.JID, raw types.RawMessage) {
	m.chatBuf.buffer(email, chat.String(), raw)
}

// Why: Resolves numeric WhatsApp mentions into human-readable contact names using explicit MentionedJID metadata instead of fragile regex parsing.
func (m *WAManager) resolveIncomingMentions(email string, client *whatsmeow.Client, text string, jids []string) string {
	if len(jids) == 0 {
		return text
	}

	result := text
	for _, jidStr := range jids {
		jid, _ := waTypes.ParseJID(jidStr)
		number := jid.User
		placeholder := "@" + number

		resolvedName := m.resolveMentionName(email, client, jid, number)
		if resolvedName == "" {
			continue
		}
		//Why: Only replaces the specific numeric occurrence if we have high-confidence metadata from the API.
		result = strings.ReplaceAll(result, placeholder, "@"+resolvedName)
	}
	return result
}

// Why: Collects display names for all mentioned JIDs so callers can store a human-readable list alongside the raw IDs.
func (m *WAManager) resolveIncomingMentionNames(email string, client *whatsmeow.Client, jids []string) []string {
	if len(jids) == 0 {
		return nil
	}
	var names []string
	for _, jidStr := range jids {
		jid, _ := waTypes.ParseJID(jidStr)
		name := m.resolveMentionName(email, client, jid, jid.User)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// Why: Falls back to whatsmeow contact metadata in priority order (full → push → business) and persists asynchronously so the next mention skips the API hop.
func (m *WAManager) resolveMentionName(email string, client *whatsmeow.Client, jid waTypes.JID, number string) string {
	if name := store.GetNameByWhatsAppNumber(email, number); name != "" {
		return name
	}
	contact, err := client.Store.Contacts.GetContact(context.Background(), jid)
	if err != nil {
		return ""
	}
	resolved := pickContactName(contact)
	if resolved == "" {
		return ""
	}
	go func(em, num, name string) {
		defer safego.Recover("wa-save-contact-mention")
		if err := store.SaveWhatsAppContact(context.Background(), em, num, name); err != nil {
			logger.Warnf("[WA] SaveWhatsAppContact failed for %s/%s: %v", em, num, err)
		}
	}(email, number, resolved)
	return resolved
}

func pickContactName(c waTypes.ContactInfo) string {
	if c.FullName != "" {
		return c.FullName
	}
	if c.PushName != "" {
		return c.PushName
	}
	return c.BusinessName
}
