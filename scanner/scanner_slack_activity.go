package scanner

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"time"

	"github.com/slack-go/slack"
)

func scanChannelHistoryActivity(ctx context.Context, sc *channels.SlackClient, threads []store.SlackThreadMeta) map[string]channelActivity {
	byChannel := groupThreadsByChannel(threads)
	out := make(map[string]channelActivity, len(byChannel))
	for chID, chThreads := range byChannel {
		out[chID] = fetchChannelHistoryActivity(sc, chID, chThreads)
	}
	return out
}

func groupThreadsByChannel(threads []store.SlackThreadMeta) map[string][]store.SlackThreadMeta {
	out := make(map[string][]store.SlackThreadMeta)
	for _, t := range threads {
		out[t.ChannelID] = append(out[t.ChannelID], t)
	}
	return out
}

func fetchChannelHistoryActivity(sc *channels.SlackClient, chID string, threads []store.SlackThreadMeta) channelActivity {
	// Why: skip the call entirely while the channel is in its access-failure backoff
	// window; retrying every sweep tick against a channel the bot cannot read just
	// re-triggers the same error and (without this guard) re-logs it every cycle.
	// This history optimization always runs against the bot client (see
	// scanChannelHistoryActivity), so the backoff it records is bot-kind only.
	if isChannelInaccessible(slackClientKindBot, chID) {
		return channelActivity{}
	}
	// Why: oldest=min(thread_ts), inclusive=true → 가장 오래된 추적 thread parent까지 한 호출로
	// 포착. 7일 timeout이 thread 수명을 제한하므로 페이지네이션 없이 limit=200으로 충분한 케이스가
	// 대부분이며, 누락된 parent는 호출자가 fallback으로 직접 fetch한다.
	params := &slack.GetConversationHistoryParameters{
		ChannelID: chID,
		Oldest:    minThreadTS(threads),
		Inclusive: true,
		Limit:     200,
	}
	hist, err := getConversationHistory(sc, params)
	if err != nil || hist == nil {
		if reason, ok := classifyAccessError(err); ok {
			recordChannelInaccessible(slackClientKindBot, chID, reason)
		} else {
			logger.Warnf("[SLACK] sweep: history fetch failed for channel %s: %v", chID, err)
		}
		return channelActivity{}
	}
	return buildChannelActivity(hist.Messages, trackedThreadSet(threads))
}

func trackedThreadSet(threads []store.SlackThreadMeta) map[string]struct{} {
	out := make(map[string]struct{}, len(threads))
	for _, t := range threads {
		out[t.ThreadTS] = struct{}{}
	}
	return out
}

func buildChannelActivity(messages []slack.Message, tracked map[string]struct{}) channelActivity {
	out := channelActivity{
		latestReplies: make(map[string]string, len(tracked)),
		replyCounts:   make(map[string]int, len(tracked)),
		fetched:       true,
	}
	for _, m := range messages {
		// Why: Slack은 reply가 0인 thread parent에 ThreadTimestamp를 채우지 않는다. 이전 필터
		// (ThreadTimestamp==Timestamp)는 silent parent를 인덱스에서 누락시켜 정작 skip 대상이
		// 매번 conversations.replies로 넘어갔다. trackedTS 기준으로 매칭해 silent parent가
		// latest_reply="" 로 등록되도록 한다.
		if _, ok := tracked[m.Timestamp]; !ok {
			continue
		}
		out.latestReplies[m.Timestamp] = m.LatestReply
		out.replyCounts[m.Timestamp] = m.ReplyCount
	}
	return out
}

func minThreadTS(threads []store.SlackThreadMeta) string {
	min := ""
	for _, t := range threads {
		if min == "" || t.ThreadTS < min {
			min = t.ThreadTS
		}
	}
	return min
}

// shouldSkipThreadFetch returns true only when channel-level history confirms no
// new replies exist beyond the stored last_reply_ts. Returns false (fall through
// to per-thread fetch) for: timed-out threads (handleThreadTimeout path), failed
// or partial channel fetches, missing thread parents, and any state ambiguity.
func shouldSkipThreadFetch(t store.SlackThreadMeta, activity map[string]channelActivity) bool {
	if isThreadTimedOut(t.LastActivityTS, 7*24*time.Hour) {
		return false
	}
	a, ok := activity[t.ChannelID]
	if !ok || !a.fetched {
		return false
	}
	latest, found := a.latestReplies[t.ThreadTS]
	if !found {
		return false
	}
	if latest == "" {
		return true
	}
	if t.LastTS == "" {
		return false
	}
	return latest <= t.LastTS
}
