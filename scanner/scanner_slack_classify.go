package scanner

import (
	"strings"

	"message-consolidator/logger"
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

func isGatedChannel(c slack.Channel) bool {
	return !c.IsIM && !c.IsMpIM
}

// Why: @here/@channel/@everyone deliberately do not count as involvement.
func isUserInvolved(user *store.User, aliases []string, m types.RawMessage) bool {
	return isFromUser(user, m) || isDirectlyAddressed(user, m, aliases)
}

// Why: covers all msgs fetched in the scan window (incl. full replies of in-window roots), not only post-cursor ones.
func involvedThreads(user *store.User, aliases []string, msgs []types.RawMessage) map[string]bool {
	out := make(map[string]bool)
	for _, m := range msgs {
		if isUserInvolved(user, aliases, m) {
			out[slackThreadTS(m)] = true
		}
	}
	return out
}

func isFromUser(user *store.User, m types.RawMessage) bool {
	if user.SlackID != "" && m.SenderID == user.SlackID {
		return true
	}
	return strings.EqualFold(m.Sender, user.Name) ||
		strings.EqualFold(m.Sender, user.Email) ||
		(user.SlackID != "" && m.Sender == user.SlackID)
}

// Why: the user's own non-DM message mentioning someone else becomes a "waiting reply" task (self-mentions excluded).
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

// Why: Lets operators drop noisy channels (e.g. bot-only rooms) from the history scan without leaving them.
func filterExcludedSlackChannels(chans []slack.Channel, excluded []string) []slack.Channel {
	if len(excluded) == 0 {
		return chans
	}
	skip := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "#"))
		if key == "" {
			continue
		}
		skip[key] = struct{}{}
	}
	out := make([]slack.Channel, 0, len(chans))
	for _, ch := range chans {
		if _, ok := skip[strings.ToLower(ch.Name)]; ok {
			continue
		}
		out = append(out, ch)
	}
	return out
}

func applySlackChannelExclusions(chans []slack.Channel) []slack.Channel {
	if cfg == nil || len(cfg.SlackExcludedChannels) == 0 {
		return chans
	}
	kept := filterExcludedSlackChannels(chans, cfg.SlackExcludedChannels)
	if n := len(chans) - len(kept); n > 0 {
		logger.Debugf("[SCAN] slack: excluded %d channel(s) via SLACK_EXCLUDED_CHANNELS", n)
	}
	return kept
}
