package channels

import (
	"encoding/json"
	"net/http"
	"time"

	"message-consolidator/db"
	"message-consolidator/logger"

	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
)

// ParseLineWebhook verifies the HMAC-SHA256 signature with channelSecret,
// parses the request body, and converts MessageEvents into InsertLineInboxParams.
// Non-text events and signature failures are silently skipped.
func ParseLineWebhook(channelSecret string, r *http.Request) ([]db.InsertLineInboxParams, error) {
	cb, err := webhook.ParseRequest(channelSecret, r)
	if err != nil {
		return nil, err
	}

	var out []db.InsertLineInboxParams
	for _, event := range cb.Events {
		msgEvent, ok := event.(webhook.MessageEvent)
		if !ok {
			continue
		}
		textMsg, ok := msgEvent.Message.(webhook.TextMessageContent)
		if !ok {
			continue
		}
		if textMsg.Text == "" {
			continue
		}

		chatType, chatID := resolveChatSource(msgEvent.Source)
		senderID := resolveSenderID(msgEvent.Source)
		mentionedIDs := extractMentions(textMsg)
		mentionJSON := marshalStringSlice(mentionedIDs)
		replyTo := resolveReplyTo(msgEvent)

		out = append(out, db.InsertLineInboxParams{
			LineMessageID: textMsg.Id,
			ChatType:      chatType,
			ChatID:        chatID,
			SenderID:      senderID,
			SenderName:    "",
			Text:          textMsg.Text,
			ReplyToID:     replyTo,
			MentionedIds:  mentionJSON,
			Ts:            time.UnixMilli(msgEvent.Timestamp).Unix(),
		})
	}
	return out, nil
}

func resolveChatSource(src webhook.SourceInterface) (chatType, chatID string) {
	switch s := src.(type) {
	case webhook.UserSource:
		return "user", s.UserId
	case webhook.GroupSource:
		return "group", s.GroupId
	case webhook.RoomSource:
		return "room", s.RoomId
	default:
		return "unknown", ""
	}
}

func resolveSenderID(src webhook.SourceInterface) string {
	switch s := src.(type) {
	case webhook.UserSource:
		return s.UserId
	case webhook.GroupSource:
		return s.UserId
	case webhook.RoomSource:
		return s.UserId
	default:
		return ""
	}
}

func resolveReplyTo(e webhook.MessageEvent) string {
	// LINE does not expose a reply-to message ID in the event; placeholder for future support.
	_ = e
	return ""
}

func extractMentions(msg webhook.TextMessageContent) []string {
	if msg.Mention == nil {
		return nil
	}
	var ids []string
	for _, m := range msg.Mention.Mentionees {
		switch v := m.(type) {
		case webhook.UserMentionee:
			ids = append(ids, v.UserId)
		}
	}
	return ids
}

func marshalStringSlice(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	b, err := json.Marshal(ss)
	if err != nil {
		logger.Warnf("[LINE] failed to marshal mentioned_ids: %v", err)
		return "[]"
	}
	return string(b)
}
