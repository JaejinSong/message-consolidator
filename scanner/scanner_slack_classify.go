package scanner

import (
	"strings"

	"message-consolidator/store"
	"message-consolidator/types"

	"github.com/slack-go/slack"
)

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m types.RawMessage) types.MessageCategory {
	if isOutgoingMentionToOther(channel, user, m) {
		return types.CategoryTask
	}
	if isBroadcastChannel(channel, m) {
		return types.CategoryTask
	}
	if isDirectlyAddressed(user, m, aliases) {
		return types.CategoryTask
	}
	return types.CategoryQuery
}

func isFromUser(user *store.User, m types.RawMessage) bool {
	return strings.EqualFold(m.Sender, user.Name) ||
		strings.EqualFold(m.Sender, user.Email) ||
		(user.SlackID != "" && m.Sender == user.SlackID)
}

// Why: 자기가 비-DM 채널에서 다른 사람을 멘션한 outgoing 메시지를 "waiting reply" task 로 분류 (자기 멘션은 제외)
func isOutgoingMentionToOther(channel slack.Channel, user *store.User, m types.RawMessage) bool {
	if !isFromUser(user, m) || channel.IsIM || channel.IsMpIM {
		return false
	}
	if !strings.Contains(m.Text, "<@U") {
		return false
	}
	return user.SlackID == "" || !strings.Contains(m.Text, "<@"+user.SlackID+">")
}

func isBroadcastChannel(channel slack.Channel, m types.RawMessage) bool {
	return channel.IsIM || channel.IsMpIM || isGroupMention(m.Text)
}

func isDirectlyAddressed(user *store.User, m types.RawMessage, aliases []string) bool {
	if user.SlackID != "" && strings.Contains(m.Text, "<@"+user.SlackID+">") {
		return true
	}
	return hasAliasMatch(m, aliases)
}

func isGroupMention(text string) bool {
	return strings.Contains(text, "<!here>") || strings.Contains(text, "<!channel>") || strings.Contains(text, "<!everyone>")
}

func hasAliasMatch(m types.RawMessage, aliases []string) bool {
	for _, alias := range aliases {
		if alias != "" && isAliasMatched(m.Text, m.Sender, alias) {
			return true
		}
	}
	return false
}
