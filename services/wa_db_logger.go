package services

import (
	"context"
	"encoding/json"
	"message-consolidator/db"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

// WADBLogger writes every incoming/outgoing WhatsApp message to the wa_messages table.
type WADBLogger struct {
	// ChatNameResolver resolves a human-readable name for a chat JID.
	// Wired in main.go to channels.DefaultWAManager.GetGroupName.
	ChatNameResolver func(email, chatJID string) string
}

func NewWADBLogger() *WADBLogger {
	return &WADBLogger{}
}

// Receive persists a WhatsApp message synchronously. Safe to call from any goroutine.
func (w *WADBLogger) Receive(email, chatJID string, msg types.RawMessage) {
	chatName := chatJID
	if w.ChatNameResolver != nil {
		chatName = w.ChatNameResolver(email, chatJID)
	}

	direction := "incoming"
	if msg.IsFromMe {
		direction = "outgoing"
	}

	mentionsJSON := "[]"
	if len(msg.MentionedNames) > 0 {
		if b, err := json.Marshal(msg.MentionedNames); err == nil {
			mentionsJSON = string(b)
		}
	}

	hasAttachment := int64(0)
	if msg.HasAttachment {
		hasAttachment = 1
	}
	isForwarded := int64(0)
	if msg.IsForwarded {
		isForwarded = 1
	}

	if err := store.InsertWAMessage(context.Background(), db.InsertWAMessageParams{
		MessageID:     msg.ID,
		Email:         email,
		ChatJid:       chatJID,
		ChatName:      chatName,
		Sender:        msg.Sender,
		Direction:     direction,
		Body:          msg.Text,
		ReplyTo:       msg.RepliedToUser,
		HasAttachment: hasAttachment,
		IsForwarded:   isForwarded,
		Mentions:      mentionsJSON,
		Ts:            msg.Timestamp.Unix(),
	}); err != nil {
		logger.Errorf("[wa-db] failed to insert message %s: %v", msg.ID, err)
	}
}
