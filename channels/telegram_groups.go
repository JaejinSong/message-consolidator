package channels

import (
	"context"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// bufferMessage appends raw into email→chatKey circular buffer (cap 200).
func (m *TelegramManager) bufferMessage(email, chatKey string, raw types.RawMessage) {
	m.chatBuf.buffer(email, chatKey, raw)
}

// PopMessages atomically drains every chat buffer for the given user.
func (m *TelegramManager) PopMessages(email string) map[string][]types.RawMessage {
	return m.chatBuf.pop(email)
}

func (m *TelegramManager) dropBuffer(email string) {
	m.chatBuf.drop(email)
}

// GetGroupName returns a human-friendly label for a chatKey. Resolution order:
//  1. in-memory groupCache (populated by storeEntities + hydrateDialogs)
//  2. persisted contact cache (DMs only — mirrors WhatsApp's store lookup)
//  3. live MessagesGetChats RPC for basic chats (no access_hash required)
//  4. numeric tail fallback
func (m *TelegramManager) GetGroupName(email string, chatKey string) string {
	if cached := m.cachedGroupName(chatKey); cached != "" {
		return cached
	}
	if name := m.resolveDMName(email, chatKey); name != "" {
		m.groupCache.Store(chatKey, name)
		return name
	}
	if title := m.resolveBasicChatTitle(email, chatKey); title != "" {
		m.groupCache.Store(chatKey, title)
		return title
	}
	return stripTelegramKeyPrefix(chatKey)
}

func (m *TelegramManager) cachedGroupName(chatKey string) string {
	v, ok := m.groupCache.Load(chatKey)
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func (m *TelegramManager) resolveDMName(email, chatKey string) string {
	if !strings.HasPrefix(chatKey, "tg_user_") {
		return ""
	}
	uid := strings.TrimPrefix(chatKey, "tg_user_")
	return store.GetNameByTelegramID(context.Background(), email, uid)
}

func (m *TelegramManager) resolveBasicChatTitle(email, chatKey string) string {
	if !strings.HasPrefix(chatKey, "tg_chat_") {
		return ""
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(chatKey, "tg_chat_"), 10, 64)
	if err != nil {
		return ""
	}
	return m.lookupBasicChatTitle(email, id)
}

func stripTelegramKeyPrefix(chatKey string) string {
	for _, prefix := range []string{"tg_user_", "tg_chat_", "tg_channel_"} {
		if strings.HasPrefix(chatKey, prefix) {
			return strings.TrimPrefix(chatKey, prefix)
		}
	}
	return chatKey
}

// hydrateDialogs fetches the dialog list once on connect to seed groupCache and
// `contacts` with every peer we can reach, so GetGroupName resolves titles for
// dormant chats that won't push a new message through ingestMessage.
func (m *TelegramManager) hydrateDialogs(ctx context.Context, client *telegram.Client, email string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := client.API().MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      200,
	})
	if err != nil {
		logger.Warnf("[TG] hydrate dialogs failed for %s: %v", email, err)
		return
	}

	e := tg.Entities{
		Users:    make(map[int64]*tg.User),
		Chats:    make(map[int64]*tg.Chat),
		Channels: make(map[int64]*tg.Channel),
	}
	var users []tg.UserClass
	var chats []tg.ChatClass
	switch d := resp.(type) {
	case *tg.MessagesDialogs:
		users, chats = d.Users, d.Chats
	case *tg.MessagesDialogsSlice:
		users, chats = d.Users, d.Chats
	default:
		return
	}
	for _, u := range users {
		if user, ok := u.(*tg.User); ok {
			e.Users[user.ID] = user
		}
	}
	for _, c := range chats {
		switch v := c.(type) {
		case *tg.Chat:
			e.Chats[v.ID] = v
		case *tg.Channel:
			e.Channels[v.ID] = v
		}
	}
	m.storeEntities(ctx, email, e)
	logger.Infof("[TG] hydrated %d users, %d chats, %d channels for %s", len(e.Users), len(e.Chats), len(e.Channels), email)
}

// lookupBasicChatTitle performs a live gotd RPC to resolve a legacy (non-channel)
// chat's title. Channels/supergroups require an access_hash we don't have on a
// cold miss, so this path only handles tg_chat_* keys.
func (m *TelegramManager) lookupBasicChatTitle(email string, chatID int64) string {
	m.mu.RLock()
	client, ok := m.clients[email]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.API().MessagesGetChats(ctx, []int64{chatID})
	if err != nil {
		return ""
	}
	for _, c := range resp.GetChats() {
		if chat, ok := c.(*tg.Chat); ok && chat.ID == chatID && chat.Title != "" {
			return chat.Title
		}
	}
	return ""
}
