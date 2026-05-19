package channels

import (
	"context"
	"fmt"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strconv"
	"time"

	"github.com/gotd/td/tg"
)

// newDispatcher builds the per-user UpdateDispatcher. Registered in startClient
// via telegram.Options.UpdateHandler so the gotd client invokes it on every push.
func (m *TelegramManager) newDispatcher(email string) tg.UpdateDispatcher {
	d := tg.NewUpdateDispatcher()
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		m.ingestMessage(ctx, email, e, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		m.ingestMessage(ctx, email, e, u.Message)
		return nil
	})
	return d
}

// ingestMessage narrows MessageClass to *tg.Message (skips MessageService/Empty)
// and pushes a normalized RawMessage into the per-chat buffer.
func (m *TelegramManager) ingestMessage(ctx context.Context, email string, e tg.Entities, mc tg.MessageClass) {
	msg, ok := mc.(*tg.Message)
	if !ok || msg.Message == "" {
		return
	}
	chatKey, ok := peerKey(msg.PeerID)
	if !ok {
		return
	}
	go func() {
		defer safego.Recover("tg-store-entities")
		m.storeEntities(ctx, email, e)
	}()
	raw := m.parseMessage(ctx, email, e, msg)
	m.bufferMessage(email, chatKey, raw)
	logger.Debugf("[TG] event for %s: %s: %s", email, chatKey, raw.Text)
}

// parseMessage maps a *tg.Message into types.RawMessage. Sender display name
// resolves from the entities bundle first, then falls back to the persisted
// contact cache so the scanner never sees a bare numeric ID when we've seen
// this user before.
func (m *TelegramManager) parseMessage(ctx context.Context, email string, e tg.Entities, msg *tg.Message) types.RawMessage {
	senderID, senderName := resolveSender(e, msg)
	if senderName == "" && senderID != "" {
		senderName = store.GetNameByTelegramID(ctx, email, senderID)
	}
	var replyToID string
	if h, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok {
		if id, have := h.GetReplyToMsgID(); have {
			replyToID = strconv.Itoa(id)
		}
	}

	return types.RawMessage{
		ID:            strconv.Itoa(msg.ID),
		Sender:        senderID,
		SenderName:    senderName,
		Text:          msg.Message,
		Timestamp:     time.Unix(int64(msg.Date), 0),
		ReplyToID:     replyToID,
		IsFromMe:      msg.Out,
		HasAttachment: msg.Media != nil,
	}
}

// storeEntities walks the dispatched users/chats/channels and (a) persists user
// display names to `contacts` for auto-retroactive v_messages resolution,
// (b) caches chat/channel titles in groupCache for GetGroupName. Runs async.
func (m *TelegramManager) storeEntities(ctx context.Context, email string, e tg.Entities) {
	for id, u := range e.Users {
		name := buildUserDisplayName(u)
		uid := strconv.FormatInt(id, 10)
		if name != "" {
			_ = store.SaveTelegramContact(ctx, email, uid, name)
			m.groupCache.Store(fmt.Sprintf("tg_user_%d", id), name)
		}
	}
	for id, c := range e.Chats {
		if c.Title != "" {
			m.groupCache.Store(fmt.Sprintf("tg_chat_%d", id), c.Title)
		}
	}
	for id, c := range e.Channels {
		if c.Title != "" {
			m.groupCache.Store(fmt.Sprintf("tg_channel_%d", id), c.Title)
		}
	}
}

func buildUserDisplayName(u *tg.User) string {
	name := u.FirstName
	if u.LastName != "" {
		if name != "" {
			name += " "
		}
		name += u.LastName
	}
	if name == "" {
		name = u.Username
	}
	return name
}

// peerKey converts the message's PeerID into a stable scanner-facing string key.
// Prefixes ("tg_user_" / "tg_chat_" / "tg_channel_") distinguish DM vs group later.
func peerKey(p tg.PeerClass) (string, bool) {
	switch v := p.(type) {
	case *tg.PeerUser:
		return fmt.Sprintf("tg_user_%d", v.UserID), true
	case *tg.PeerChat:
		return fmt.Sprintf("tg_chat_%d", v.ChatID), true
	case *tg.PeerChannel:
		return fmt.Sprintf("tg_channel_%d", v.ChannelID), true
	default:
		return "", false
	}
}

// resolveSender returns (senderID, senderName). Missing FromID falls back to PeerID
// (DM case where the whole chat is the sender).
func resolveSender(e tg.Entities, msg *tg.Message) (string, string) {
	if from, ok := msg.GetFromID(); ok {
		if pu, ok := from.(*tg.PeerUser); ok {
			return strconv.FormatInt(pu.UserID, 10), userName(e, pu.UserID)
		}
	}
	if pu, ok := msg.PeerID.(*tg.PeerUser); ok {
		return strconv.FormatInt(pu.UserID, 10), userName(e, pu.UserID)
	}
	return "", ""
}

func userName(e tg.Entities, id int64) string {
	u, ok := e.Users[id]
	if !ok {
		return ""
	}
	return buildUserDisplayName(u)
}
