package scanner

import (
	"context"
	"fmt"
	"message-consolidator/channels"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"golang.org/x/sync/errgroup"
)

var slackMentionRegex = regexp.MustCompile(`<@([A-Z0-9]+)>`)

// Why: extract <@USERID> mentions in document order; preserves first-mention primacy
//
//	so resolveAssignee can pick a primary actor when AI returns "shared".
func extractSlackMentionUserIDs(text string) []string {
	matches := slackMentionRegex.FindAllStringSubmatch(text, -1)
	ids := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		ids = append(ids, m[1])
	}
	return ids
}

// Why: resolves a list of Slack user IDs to display names via SlackClient cache + on-demand
//
//	GetUserInfo; unresolved IDs are dropped (caller treats empty list as no-mention).
func resolveSlackMentionNames(ctx context.Context, sc slackUserResolver, userIDs []string) []string {
	out := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		name := sc.GetUserName(ctx, id)
		if name == "" || name == id {
			continue
		}
		out = append(out, name)
	}
	return out
}


// Why: SlackClient + botID + users.list 결과는 토큰 단위로 불변. 매 sweep마다 NewSlackClient/FetchUsers/AuthTest를
// 다시 부르면 sweep당 ~250ms (users.list ×2 + auth.test) 낭비. 토큰 키 캐시로 한 번만 초기화한다.
var (
	slackClientMu     sync.Mutex
	cachedSlackToken  string
	cachedSlackClient *channels.SlackClient
	cachedSlackBotID  string
)

func getOrInitSlackClient(token string) (*channels.SlackClient, string) {
	slackClientMu.Lock()
	defer slackClientMu.Unlock()
	if cachedSlackClient != nil && cachedSlackToken == token {
		return cachedSlackClient, cachedSlackBotID
	}
	c := channels.NewSlackClient(token)
	_ = c.FetchUsers()
	botID := ""
	if a, _ := c.GetAPI().AuthTest(); a != nil {
		botID = a.UserID
	}
	cachedSlackToken = token
	cachedSlackClient = c
	cachedSlackBotID = botID
	return c, botID
}

type slackThreadIdentity struct {
	user       *store.User
	effAliases []string
}

type slackUserResolver interface {
	GetUserName(ctx context.Context, userID string) string
}

func resolveSlackMentions(ctx context.Context, text string, sc slackUserResolver) string {
	return slackMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		userID := match[2 : len(match)-1]
		userName := sc.GetUserName(ctx, userID)
		if userName != "" && userName != userID {
			return "@" + userName
		}
		return match
	})
}

func scanSlack(ctx context.Context, users []store.User, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" || len(users) == 0 {
		return
	}
	sc, _ := getOrInitSlackClient(cfg.SlackToken) //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.

	chans, _, err := sc.LookupChannels()
	if err != nil {
		logger.Errorf("[SCAN] slack: failed to fetch channels: %v", err)
		return
	}

	userAl := prepareSlackUserAliases(ctx, users)
	candidates, newTS := collectSlackHistory(ctx, users, chans, sc, userAl)
	processSlackCandidates(ctx, users, sc, candidates, wg)
	updateSlackCursors(newTS)

	//Why: Forces immediate persistence of scan cursors after each cycle to prevent data loss or scan gaps in case of process termination.
	for _, u := range users {
		store.PersistAllScanMetadata(ctx, u.Email)
	}
}

func prepareSlackUserAliases(ctx context.Context, users []store.User) map[string][]string {
	ua := make(map[string][]string)
	for _, u := range users {
		aliases, _ := store.GetUserAliases(ctx, u.ID)
		ua[u.Email] = services.GetEffectiveAliases(u, aliases)
	}
	return ua
}

func collectSlackHistory(ctx context.Context, users []store.User, chans []slack.Channel, sc *channels.SlackClient, userAl map[string][]string) (map[string][]types.RawMessage, map[string]map[string]string) {
	candidates := make(map[string][]types.RawMessage)
	newTS := make(map[string]map[string]string)
	var mu sync.Mutex

	var eg errgroup.Group
	eg.SetLimit(3)
	for _, ch := range chans {
		c := ch
		eg.Go(func() error {
			return scanSingleSlackChannel(ctx, users, c, sc, userAl, &mu, candidates, newTS)
		})
	}
	_ = eg.Wait()
	return candidates, newTS
}

func scanSingleSlackChannel(ctx context.Context, users []store.User, c slack.Channel, sc *channels.SlackClient, userAl map[string][]string, mu *sync.Mutex, candidates map[string][]types.RawMessage, newTS map[string]map[string]string) error {
	minTS := getMinLastTS(users, c.ID)
	logger.Debugf("[SLACK] channel %s: minTS=%s", c.ID, minTS)
	//Why: Uses a dual-strategy scan window. It scans up to 24 hours back by default,
	// but respects minTS as a lower bound only if it provides a safer (older) starting point,
	// preventing "islands" of unproccessed messages between scan intervals.
	since := time.Now().Add(-24 * time.Hour)
	msgs, err := sc.GetMessages(ctx, c.ID, since, minTS)
	if err != nil {
		logger.Errorf("[SCAN] slack: GetMessages failed for channel %s: %v", c.ID, err)
		return err
	}
	if len(msgs) == 0 {
		logger.Debugf("[SLACK] channel %s: no new messages (minTS: %s)", c.ID, minTS)
		return nil
	}

	mu.Lock()
	defer mu.Unlock()
	for _, m := range msgs {
		classifyAndCollect(ctx, c, sc, m, users, userAl, candidates, newTS)
	}
	return nil
}

func getMinLastTS(users []store.User, channelID string) string {
	min := ""
	for _, u := range users {
		ts := store.GetLastScan(u.Email, "slack", channelID)
		if ts == "" {
			return ""
		}
		if min == "" || ts < min {
			min = ts
		}
	}
	return min
}

func classifyAndCollect(ctx context.Context, c slack.Channel, sc *channels.SlackClient, m types.RawMessage, users []store.User, userAl map[string][]string, candidates map[string][]types.RawMessage, newTS map[string]map[string]string) {
	m.ChannelID = c.ID
	for _, u := range users {
		lts := store.GetLastScan(u.Email, "slack", c.ID)
		if lts != "" && m.ID <= lts {
			continue
		}
		dispatchOutgoingCompletionIfMine(ctx, sc, u, m)
		cls := classifyMessage(c, &u, userAl[u.Email], m)
		if cls == types.CategoryTask || cls == types.CategoryQuery {
			candidates[u.Email] = append(candidates[u.Email], m)
		}
		updateChannelCursor(newTS, u.Email, c.ID, m.ID)
	}
}

// Why: When the user replies in their own thread we evaluate state (RESOLVE/UPDATE) on a Background ctx so Gemini latency doesn't block the scan loop.
// _ ctx is accepted for trace propagation parity with the rest of classifyAndCollect; the goroutine itself uses Background.
func dispatchOutgoingCompletionIfMine(_ context.Context, sc *channels.SlackClient, u store.User, m types.RawMessage) {
	if completionSvc == nil || m.ReplyToID == "" {
		return
	}
	if !strings.EqualFold(m.Sender, u.Name) && !strings.EqualFold(m.Sender, u.Email) {
		return
	}
	// Why: completion-fallback may INSERT a new task when the thread has no
	// open parent. Propagate envelope (Requester/Room/Link/AssignedAt/SourceChannels)
	// so the resulting row matches the normal scanner path instead of empty fields.
	room := sc.GetChannelName(m.ChannelID)
	link := buildSlackLink(m)
	go func(bgCtx context.Context, email string, raw types.RawMessage, room, link string) { //nolint:contextcheck // Goroutine outlives the parent scan ctx by design.
		defer safego.Recover("slack-outgoing-completion")
		if _, err := completionSvc.ProcessPotentialCompletion(bgCtx, store.ConsolidatedMessage{
			UserEmail: email, Source: "slack",
			Room: room, Link: link,
			Requester: raw.Sender, RequesterCanonical: email,
			AssignedAt: raw.Timestamp, CreatedAt: raw.Timestamp,
			ThreadID: raw.ReplyToID, RepliedToID: raw.ReplyToID,
			OriginalText: raw.Text, SourceTS: raw.ID,
			SourceChannels: []string{"slack"},
		}); err != nil {
			logger.Warnf("[SLACK] outgoing completion failed for %s: %v", email, err)
		}
	}(context.Background(), u.Email, m, room, link)
}

func updateChannelCursor(newTS map[string]map[string]string, email, channelID, msgID string) {
	if newTS[email] == nil {
		newTS[email] = make(map[string]string)
	}
	if curr, ok := newTS[email][channelID]; !ok || msgID > curr {
		newTS[email][channelID] = msgID
	}
}

func processSlackCandidates(ctx context.Context, users []store.User, sc *channels.SlackClient, candidates map[string][]types.RawMessage, wg *sync.WaitGroup) {
	for email, msgs := range candidates {
		user, err := store.GetOrCreateUser(ctx, email, "", "")
		if err != nil || user == nil {
			continue
		}
		logger.Debugf("[SLACK] user %s: %d candidates queued for AI analysis", email, len(msgs))
		analyzeAndSaveSlack(ctx, user, sc, msgs, wg)
	}
}

func updateSlackCursors(newTS map[string]map[string]string) {
	for email, channelMap := range newTS {
		for chanID, ts := range channelMap {
			if err := store.UpdateLastScan(email, "slack", chanID, ts); err != nil {
				logger.Warnf("[SCAN] slack: UpdateLastScan failed for %s/%s: %v", email, chanID, err)
			}
		}
	}
}

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

type channelActivity struct {
	latestReplies map[string]string
	replyCounts   map[string]int
	fetched       bool
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

type threadScanResult struct {
	isResolved      bool
	newLastTS       string
	newLastActivity string
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

func analyzeAndSaveSlack(ctx context.Context, user *store.User, sc *channels.SlackClient, candidates []types.RawMessage, wg *sync.WaitGroup) {
	if len(candidates) == 0 {
		return
	}
	if gClient == nil {
		logger.Errorf("[SCAN] slack: gClient not initialized; scanner.Init may have failed")
		return
	}

	channelName := sc.GetChannelName(candidates[0].ChannelID)
	lockKey := roomLockSvc.GetRoomKey(user.Email, "slack", channelName)
	lock := roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	payload, msgMap := buildSlackAnalysisPayload(ctx, candidates, sc)
	lastMsg := candidates[len(candidates)-1]
	senderName := lastMsg.SenderName
	if senderName == "" {
		senderName = lastMsg.Sender
	}
	enriched, _ := EnrichSlackMessage(lastMsg.Sender, senderName, lastMsg.ChannelID, lastMsg.ReplyToID, payload, lastMsg.Timestamp)
	if strings.HasPrefix(candidates[0].ChannelID, "D") {
		enriched.ChatType = "1to1"
	} else {
		enriched.ChatType = "group"
	}

	proposals, err := gClient.Analyze(ctx, user.Email, *enriched, "Korean", "slack", channelName)
	if err != nil {
		logger.Errorf("[SCAN] slack: Gemini analyze error for %s: %v", user.Email, err)
		return
	}

	// Why: inject thread context so findMatch can guard against cross-thread merges.
	for i := range proposals {
		if raw, ok := msgMap[proposals[i].SourceTS]; ok {
			proposals[i].ThreadID = raw.ThreadID
		}
	}

	// Why: [Service-Oriented Resolve] Ensures SLACK proposals are resolved using the same backend-driven similarity engine.
	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, "slack", channelName)
	items := tasksSvc.ResolveProposals(ctx, user.Email, channelName, proposals, tasks)
	processSlackItems(ctx, user, items, msgMap, sc, wg)
}

func processSlackItems(ctx context.Context, user *store.User, items []store.TodoItem, msgMap map[string]types.RawMessage, sc *channels.SlackClient, wg *sync.WaitGroup) {
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	var newIDs []store.MessageID
	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			continue
		}
		msg := mapSlackItemToMessage(ctx, item, m, user, aliases, sc)

		id, err := services.HandleTaskState(ctx, nil, user.Email, item, msg)
		if err == nil && id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
}

func buildSlackAnalysisPayload(ctx context.Context, candidates []types.RawMessage, sc *channels.SlackClient) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range candidates {
		msgMap[m.ID] = m
		resolvedText := resolveSlackMentions(ctx, m.Text, sc)
		metaStr := buildSlackMetadataString(m)
		senderLabel := m.SenderName
		if senderLabel == "" {
			senderLabel = m.Sender
		}
		var tsTag string
		if !m.Timestamp.IsZero() {
			tsTag = fmt.Sprintf("[ts:%s]", m.Timestamp.UTC().Format("2006-01-02T15:04"))
		}
		sb.WriteString(fmt.Sprintf("[ID:%s]%s%s %s: %s\n", m.ID, tsTag, metaStr, senderLabel, resolvedText))
	}
	return sb.String(), msgMap
}

func buildSlackMetadataString(m types.RawMessage) string {
	var tags []string
	if m.IsPinned {
		tags = append(tags, "Pinned")
	}
	if m.IsImportant {
		tags = append(tags, "Important")
	}
	if m.IsForwarded {
		tags = append(tags, "Forwarded")
	}
	var sb strings.Builder
	if len(tags) > 0 {
		sb.WriteString(fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", ")))
	}
	if len(m.Reactions) > 0 {
		sb.WriteString(fmt.Sprintf(" [Reactions: %s]", strings.Join(m.Reactions, ", ")))
	}
	if len(m.AttachmentNames) > 0 {
		sb.WriteString(fmt.Sprintf(" [Files: %s]", strings.Join(m.AttachmentNames, ", ")))
	}
	return sb.String()
}

func mapSlackItemToMessage(ctx context.Context, item store.TodoItem, m types.RawMessage, user *store.User, aliases []string, sc *channels.SlackClient) store.ConsolidatedMessage {
	threadID := m.ReplyToID
	if threadID == "" {
		threadID = m.ID
	}
	link := buildSlackLinkAndRegisterThread(ctx, m, user.Email)
	mentionIDs := extractSlackMentionUserIDs(m.Text)
	explicitMentions := resolveSlackMentionNames(ctx, sc, mentionIDs)

	params := services.TaskBuildParams{
		UserEmail:        user.Email,
		User:             *user,
		Aliases:          aliases,
		Item:             item,
		SenderRaw:        m.Sender, // Resolved Slack display name — primary identity fallback
		Source:           "slack",
		Room:             sc.GetChannelName(m.ChannelID),
		Link:             link,
		SourceTS:         m.ID,
		Timestamp:        m.Timestamp,
		OriginalText:     m.Text,
		ThreadID:         threadID,
		RepliedToID:      m.ReplyToID,
		SourceChannels:   []string{"slack"},
		ExplicitMentions: explicitMentions,
	}
	return services.BuildTask(ctx, params)
}

func buildSlackLink(m types.RawMessage) string {
	link := fmt.Sprintf("https://slack.com/archives/%s/p%s", m.ChannelID, strings.ReplaceAll(m.ID, ".", ""))
	if m.ReplyToID != "" {
		link += fmt.Sprintf("?thread_ts=%s", m.ReplyToID)
	}
	return link
}

func slackThreadTS(m types.RawMessage) string {
	if m.ReplyToID != "" {
		return m.ReplyToID
	}
	return m.ID
}

func buildSlackLinkAndRegisterThread(ctx context.Context, m types.RawMessage, email string) string {
	link := buildSlackLink(m)
	threadTS := slackThreadTS(m)
	// Why: register both parent messages and replies so slow sweeper always tracks future activity.
	if err := store.RegisterTargetedSlackThread(ctx, m.ChannelID, threadTS, m.ID, email); err == nil {
		logger.Debugf("[SLACK] thread registered for tracking: %s (user: %s)", threadTS, email)
	}
	return link
}
