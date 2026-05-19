package scanner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"

	"github.com/slack-go/slack"
)

type slackThreadIdentity struct {
	user       *store.User
	effAliases []string
}

type channelActivity struct {
	latestReplies map[string]string
	replyCounts   map[string]int
	fetched       bool
}

type threadScanResult struct {
	isResolved      bool
	newLastTS       string
	newLastActivity string
}

// sweepSlackThreads is invoked by the prime-loop scheduler (see runSlackSweep in scanner.go);
// the loop owns the trace span, so this function does not start its own.
func sweepSlackThreads(ctx context.Context, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" {
		return
	}
	threads, err := store.GetTargetedActiveThreads(ctx)
	if err != nil || len(threads) == 0 {
		return
	}
	sc, botID := getOrInitSlackClient(cfg.SlackToken)
	aliasCache := buildSlackAliasCache(ctx, threads)
	activity := scanChannelHistoryActivity(ctx, sc, threads)

	for _, group := range groupThreadsByKey(threads) {
		rep := group[0]
		if shouldSkipThreadFetch(rep, activity) {
			continue
		}
		if isThreadTimedOut(rep.LastActivityTS, 7*24*time.Hour) {
			handleThreadTimeoutGroup(ctx, sc, group)
			continue
		}
		processSlackThreadGroup(ctx, sc, group, botID, aliasCache, wg)
	}
}

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
	// Why: oldest=min(thread_ts), inclusive=true → 가장 오래된 추적 thread parent까지 한 호출로
	// 포착. 7일 timeout이 thread 수명을 제한하므로 페이지네이션 없이 limit=200으로 충분한 케이스가
	// 대부분이며, 누락된 parent는 호출자가 fallback으로 직접 fetch한다.
	params := &slack.GetConversationHistoryParameters{
		ChannelID: chID,
		Oldest:    minThreadTS(threads),
		Inclusive: true,
		Limit:     200,
	}
	var hist *slack.GetConversationHistoryResponse
	err := channels.WithSlackRetry(3, fmt.Sprintf("history %s", chID), func() error {
		var e error
		hist, e = sc.GetAPI().GetConversationHistory(params)
		return e
	})
	if err != nil || hist == nil {
		logger.Warnf("[SLACK] sweep: history fetch failed for channel %s: %v", chID, err)
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

func buildSlackAliasCache(ctx context.Context, threads []store.SlackThreadMeta) map[string]slackThreadIdentity {
	out := make(map[string]slackThreadIdentity, len(threads))
	for _, t := range threads {
		if _, ok := out[t.UserEmail]; ok {
			continue
		}
		u, _ := store.GetOrCreateUser(ctx, t.UserEmail, "", "")
		if u == nil {
			out[t.UserEmail] = slackThreadIdentity{}
			continue
		}
		al, _ := store.GetUserAliases(ctx, u.ID)
		out[t.UserEmail] = slackThreadIdentity{user: u, effAliases: services.GetEffectiveAliases(*u, al)}
	}
	return out
}

func groupThreadsByKey(threads []store.SlackThreadMeta) [][]store.SlackThreadMeta {
	type key struct{ channelID, threadTS string }
	indexMap := make(map[key]int, len(threads))
	var groups [][]store.SlackThreadMeta
	for _, t := range threads {
		k := key{t.ChannelID, t.ThreadTS}
		if i, ok := indexMap[k]; ok {
			groups[i] = append(groups[i], t)
		} else {
			indexMap[k] = len(groups)
			groups = append(groups, []store.SlackThreadMeta{t})
		}
	}
	return groups
}

func processSingleSlackThread(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta, botID string, aliasCache map[string]slackThreadIdentity, wg *sync.WaitGroup) {
	if isThreadTimedOut(t.LastActivityTS, 7*24*time.Hour) {
		handleThreadTimeout(ctx, sc, t)
		return
	}

	ident, ok := aliasCache[t.UserEmail]
	if !ok || ident.user == nil {
		return
	}
	user := ident.user

	params := &slack.GetConversationRepliesParameters{
		ChannelID: t.ChannelID, Timestamp: t.ThreadTS, Oldest: t.LastTS, Limit: 100,
	}
	var replies []slack.Message
	err := channels.WithSlackRetry(3, fmt.Sprintf("thread %s/%s", t.ChannelID, t.ThreadTS), func() error {
		var e error
		replies, _, _, e = sc.GetAPI().GetConversationReplies(params)
		return e
	})
	if err != nil {
		return
	}

	res := scanThreadReplies(replies, t.LastTS, t.LastActivityTS, botID)
	candidates := collectThreadCandidates(ctx, sc, user, t, replies, res, ident.effAliases)

	if len(candidates) > 0 {
		analyzeAndSaveSlack(ctx, user, sc, candidates, wg)
	}
	updateThreadStatus(ctx, sc, t, res)
}

func processSlackThreadGroup(ctx context.Context, sc *channels.SlackClient, group []store.SlackThreadMeta, botID string, aliasCache map[string]slackThreadIdentity, wg *sync.WaitGroup) {
	rep := group[0]
	minLastTS := rep.LastTS
	for _, s := range group[1:] {
		if s.LastTS != "" && (minLastTS == "" || s.LastTS < minLastTS) {
			minLastTS = s.LastTS
		}
	}

	params := &slack.GetConversationRepliesParameters{
		ChannelID: rep.ChannelID, Timestamp: rep.ThreadTS, Oldest: minLastTS, Limit: 100,
	}
	var replies []slack.Message
	err := channels.WithSlackRetry(3, fmt.Sprintf("thread %s/%s", rep.ChannelID, rep.ThreadTS), func() error {
		var e error
		replies, _, _, e = sc.GetAPI().GetConversationReplies(params)
		return e
	})
	if err != nil {
		return
	}

	res := scanThreadReplies(replies, minLastTS, rep.LastActivityTS, botID)
	for _, sub := range group {
		ident, ok := aliasCache[sub.UserEmail]
		if !ok || ident.user == nil {
			continue
		}
		candidates := collectThreadCandidates(ctx, sc, ident.user, sub, replies, res, ident.effAliases)
		if len(candidates) > 0 {
			analyzeAndSaveSlack(ctx, ident.user, sc, candidates, wg)
		}
	}
	updateThreadStatusGroup(ctx, sc, group, res)
}

func handleThreadTimeout(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta) {
	if t.ThreadTS == "" {
		logger.Warnf("[SLACK] handleThreadTimeout: empty ThreadTS channel=%s user=%s, closing without posting", t.ChannelID, t.UserEmail)
		_ = store.CloseTargetedThread(ctx, t.ChannelID, t.ThreadTS, t.UserEmail)
		return
	}
	_ = store.CloseTargetedThread(ctx, t.ChannelID, t.ThreadTS, t.UserEmail)
}

func handleThreadTimeoutGroup(ctx context.Context, sc *channels.SlackClient, group []store.SlackThreadMeta) {
	rep := group[0]
	if rep.ThreadTS == "" {
		for _, s := range group {
			logger.Warnf("[SLACK] handleThreadTimeout: empty ThreadTS channel=%s user=%s, closing without posting", s.ChannelID, s.UserEmail)
			_ = store.CloseTargetedThread(ctx, s.ChannelID, s.ThreadTS, s.UserEmail)
		}
		return
	}
	for _, s := range group {
		_ = store.CloseTargetedThread(ctx, s.ChannelID, s.ThreadTS, s.UserEmail)
	}
}

func collectThreadCandidates(ctx context.Context, sc *channels.SlackClient, user *store.User, t store.SlackThreadMeta, replies []slack.Message, res threadScanResult, effAl []string) []types.RawMessage {
	var candidates []types.RawMessage
	c := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: t.ChannelID}}}
	for _, m := range replies {
		if t.LastTS != "" && m.Timestamp <= t.LastTS {
			continue
		}
		if res.isResolved && m.Timestamp > res.newLastTS {
			continue
		}
		if m.BotID != "" || m.SubType == "bot_message" {
			continue
		}
		dispatchThreadCompletionIfMine(ctx, sc, user, t, m)
		// Architecture Separation: bot/empty pre-filters live in the channels layer.
		cls := classifyMessage(c, user, effAl, types.RawMessage{Sender: m.User, Text: m.Text})
		if cls != types.CategoryTask && cls != types.CategoryQuery {
			continue
		}
		candidates = append(candidates, types.RawMessage{
			ID: m.Timestamp, Sender: sc.GetUserName(ctx, m.User), Text: m.Text, Timestamp: channels.ParseSlackTimestamp(m.Timestamp),
			ReplyToID: t.ThreadTS, ChannelID: t.ChannelID, HasAttachment: len(m.Files) > 0,
			AttachmentNames: sc.ExtractFileNames(m.Files), Reactions: sc.ExtractReactions(m.Reactions), IsPinned: len(m.PinnedTo) > 0,
		})
	}
	return candidates
}

func dispatchThreadCompletionIfMine(ctx context.Context, sc *channels.SlackClient, user *store.User, t store.SlackThreadMeta, m slack.Message) {
	if completionSvc == nil || m.ThreadTimestamp == "" {
		return
	}
	if !strings.EqualFold(m.User, user.SlackID) && sc.GetUserName(ctx, m.User) != user.Name {
		return
	}
	if _, err := completionSvc.ProcessPotentialCompletion(ctx, store.ConsolidatedMessage{
		UserEmail: user.Email, Source: "slack", ThreadID: t.ThreadTS, OriginalText: m.Text, SourceTS: m.Timestamp,
		RequesterCanonical: user.Email,
	}); err != nil {
		logger.Warnf("[SLACK] thread completion failed for %s: %v", user.Email, err)
	}
}

func updateThreadStatus(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta, res threadScanResult) {
	if res.isResolved {
		if t.ThreadTS == "" {
			logger.Warnf("[SLACK] updateThreadStatus: empty ThreadTS channel=%s user=%s, skipping PostMessage", t.ChannelID, t.UserEmail)
		} else {
			msg := "This issue has been marked as resolved and monitoring is closed."
			_, _, _ = sc.GetAPI().PostMessage(t.ChannelID, slack.MsgOptionText(msg, false), slack.MsgOptionTS(t.ThreadTS))
		}
		_ = store.CloseTargetedThread(ctx, t.ChannelID, t.ThreadTS, t.UserEmail)
		return
	}
	if res.newLastTS != t.LastTS || res.newLastActivity != t.LastActivityTS {
		_ = store.UpdateTargetedThread(ctx, t.ChannelID, t.ThreadTS, res.newLastTS, res.newLastActivity, t.UserEmail)
	}
}

func updateThreadStatusGroup(ctx context.Context, sc *channels.SlackClient, group []store.SlackThreadMeta, res threadScanResult) {
	if res.isResolved {
		rep := group[0]
		if rep.ThreadTS == "" {
			logger.Warnf("[SLACK] updateThreadStatus: empty ThreadTS channel=%s, skipping PostMessage", rep.ChannelID)
		} else {
			msg := "This issue has been marked as resolved and monitoring is closed."
			_, _, _ = sc.GetAPI().PostMessage(rep.ChannelID, slack.MsgOptionText(msg, false), slack.MsgOptionTS(rep.ThreadTS))
		}
		for _, s := range group {
			_ = store.CloseTargetedThread(ctx, s.ChannelID, s.ThreadTS, s.UserEmail)
		}
		return
	}
	for _, s := range group {
		updateThreadStatus(ctx, sc, s, res)
	}
}

func scanThreadReplies(replies []slack.Message, lastTS, lastActivityTS, botID string) threadScanResult {
	newLastTS := lastTS
	newLastActivity := lastActivityTS
	isResolved := false

	for _, m := range replies {
		if lastTS != "" && m.Timestamp <= lastTS {
			continue
		}
		if hasResolvedReaction(m) {
			isResolved = true
		}
		if !isBotAuthor(m, botID) && !isResolved && m.Timestamp > newLastActivity {
			newLastActivity = m.Timestamp
		}
		if m.Timestamp > newLastTS {
			newLastTS = m.Timestamp
		}
		if isResolved {
			break
		}
	}
	return threadScanResult{isResolved: isResolved, newLastTS: newLastTS, newLastActivity: newLastActivity}
}

func hasResolvedReaction(m slack.Message) bool {
	for _, r := range m.Reactions {
		if r.Name == "white_check_mark" {
			return true
		}
	}
	return false
}

func isBotAuthor(m slack.Message, botID string) bool {
	return m.User == botID || m.BotID != ""
}

func isThreadTimedOut(lastActivityTS string, threshold time.Duration) bool {
	sec, err := strconv.ParseInt(strings.Split(lastActivityTS, ".")[0], 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(sec, 0)) > threshold
}
