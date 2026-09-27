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

// coldTierInterval bounds how often the cold reconciliation tier (below) may run.
// Why: it re-scans every user's stale-but-still-open threads and is far more expensive
// than the hot sweep, so it rides a slower, independent cadence gated in-memory.
const coldTierInterval = 173 * time.Minute

var (
	coldTierMu      sync.Mutex
	lastColdTierRun time.Time
)

// shouldRunColdTier reports whether coldTierInterval has elapsed since the last cold
// tier pass, and if so claims the slot immediately (guarded by coldTierMu) so concurrent
// sweep ticks cannot both start a pass.
func shouldRunColdTier(now time.Time) bool {
	coldTierMu.Lock()
	defer coldTierMu.Unlock()
	if now.Sub(lastColdTierRun) < coldTierInterval {
		return false
	}
	lastColdTierRun = now
	return true
}

// sweepSlackThreads is invoked by the prime-loop scheduler (see runSlackSweep in scanner.go);
// the loop owns the trace span, so this function does not start its own.
func sweepSlackThreads(ctx context.Context, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" {
		return
	}
	sc, botID := getOrInitSlackClient(cfg.SlackToken) //nolint:contextcheck // whataphttpx.Client takes no ctx by design; trace rides on http.Request.Context (see package doc)
	budget := newThreadReplyBudget()

	threads, err := store.GetTargetedActiveThreads(ctx)
	if err == nil && len(threads) > 0 {
		sweepHotSlackThreads(ctx, sc, botID, threads, wg, budget)
	}

	if shouldRunColdTier(time.Now()) {
		sweepColdReconciliationThreads(ctx, sc, botID, wg, budget)
	}
}

func sweepHotSlackThreads(ctx context.Context, sc *channels.SlackClient, botID string, threads []store.SlackThreadMeta, wg *sync.WaitGroup, budget *threadReplyBudget) {
	aliasCache := buildSlackAliasCache(ctx, threads)
	activity := scanChannelHistoryActivity(ctx, sc, threads)

	for _, group := range groupThreadsByKey(threads) {
		rep := group[0]
		if isChannelInaccessible(rep.ChannelID) {
			continue
		}
		if shouldSkipThreadFetch(rep, activity) {
			continue
		}
		if isThreadTimedOut(rep.LastActivityTS, 7*24*time.Hour) {
			handleThreadTimeoutGroup(ctx, sc, group)
			continue
		}
		processSlackThreadGroup(ctx, sc, group, botID, aliasCache, wg, budget)
	}
}

// sweepColdReconciliationThreads re-checks threads whose slack_threads row already
// timed out (status != 'active') but whose linked task is still open. It reuses the
// hot sweep's fetch+dispatch path (fetchAndDispatchThreadGroup) so completion detection
// stays in one place; it never reactivates slack_threads status and never applies the
// 7-day timeout that the hot loop uses.
func sweepColdReconciliationThreads(ctx context.Context, sc *channels.SlackClient, botID string, wg *sync.WaitGroup, budget *threadReplyBudget) {
	threads, err := store.GetColdReconciliationThreads(ctx)
	if err != nil || len(threads) == 0 {
		return
	}
	aliasCache := buildSlackAliasCache(ctx, threads)
	for _, group := range groupThreadsByKey(threads) {
		rep := group[0]
		if isChannelInaccessible(rep.ChannelID) {
			continue
		}
		processColdReconciliationGroup(ctx, sc, group, botID, aliasCache, wg, budget)
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
	// Why: skip the call entirely while the channel is in its access-failure backoff
	// window; retrying every sweep tick against a channel the bot cannot read just
	// re-triggers the same error and (without this guard) re-logs it every cycle.
	if isChannelInaccessible(chID) {
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
			recordChannelInaccessible(chID, reason)
		} else {
			logger.Warnf("[SLACK] sweep: history fetch failed for channel %s: %v", chID, err)
		}
		return channelActivity{}
	}
	return buildChannelActivity(hist.Messages, trackedThreadSet(threads))
}

// channelBackoffWindow bounds how long a channel identified as inaccessible (bot
// removed, channel archived/deleted, missing scope) is skipped before retry.
const channelBackoffWindow = 59 * time.Minute

// slackAccessFailureReasons are the slack-go error strings that indicate the bot can
// no longer read a channel, as opposed to a transient/unknown failure worth retrying
// every cycle with a Warn log.
var slackAccessFailureReasons = []string{"channel_not_found", "not_in_channel", "is_archived", "missing_scope"}

func classifyAccessError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg := err.Error()
	for _, reason := range slackAccessFailureReasons {
		if strings.Contains(msg, reason) {
			return reason, true
		}
	}
	return "", false
}

type inaccessibleChannelInfo struct {
	reason string
	until  time.Time
}

var (
	inaccessibleMu       sync.Mutex
	inaccessibleChannels = map[string]inaccessibleChannelInfo{}
)

// recordChannelInaccessible remembers chID as unreachable for channelBackoffWindow and
// logs exactly one Error line per channel per window (no per-thread spam).
func recordChannelInaccessible(chID, reason string) {
	inaccessibleMu.Lock()
	defer inaccessibleMu.Unlock()
	if info, ok := inaccessibleChannels[chID]; ok && time.Now().Before(info.until) {
		return
	}
	inaccessibleChannels[chID] = inaccessibleChannelInfo{reason: reason, until: time.Now().Add(channelBackoffWindow)}
	logger.Errorf("[SLACK] channel %s inaccessible (%s): bot not a member or channel gone - invite the bot to resume", chID, reason)
}

// maxThreadReplyEvalsPerSweep bounds how many LLM transition calls one sweepSlackThreads
// invocation may spend evaluating plain (non-keyword) counterparty thread replies.
// Why: production evidence showed every real completion was a same-thread reply, so
// dropping the keyword gate now sends every counterparty reply in a tracked thread to
// EvaluateThreadReply -- this caps the blast radius of one sweep tick.
const maxThreadReplyEvalsPerSweep = 29

// threadReplyBudget shares an LLM-call counter across one sweepSlackThreads run (hot
// and cold tiers alike) so the cap applies per sweep, not per thread group.
type threadReplyBudget struct {
	mu   sync.Mutex
	used int
}

func newThreadReplyBudget() *threadReplyBudget { return &threadReplyBudget{} }

// take claims one slot from the budget, returning false once the cap is reached.
func (b *threadReplyBudget) take() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= maxThreadReplyEvalsPerSweep {
		return false
	}
	b.used++
	return true
}

func isChannelInaccessible(chID string) bool {
	inaccessibleMu.Lock()
	defer inaccessibleMu.Unlock()
	info, ok := inaccessibleChannels[chID]
	return ok && time.Now().Before(info.until)
}

// InaccessibleSlackChannels exposes the current channel→reason backoff set for a
// future status endpoint. Entries past their backoff window are omitted.
func InaccessibleSlackChannels() map[string]string {
	inaccessibleMu.Lock()
	defer inaccessibleMu.Unlock()
	now := time.Now()
	out := make(map[string]string, len(inaccessibleChannels))
	for chID, info := range inaccessibleChannels {
		if now.Before(info.until) {
			out[chID] = info.reason
		}
	}
	return out
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

// getConversationHistory is a seam over the real Slack call so tests can inject fake
// channel-history responses (and failures) without a network round trip.
var getConversationHistory = defaultGetConversationHistory

func defaultGetConversationHistory(sc *channels.SlackClient, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	var hist *slack.GetConversationHistoryResponse
	err := channels.WithSlackRetry(3, fmt.Sprintf("history %s", params.ChannelID), func() error {
		var e error
		hist, e = sc.GetAPI().GetConversationHistory(params)
		return e
	})
	return hist, err
}

// getConversationReplies is a seam over the real Slack call so tests can inject fake
// replies without a network round trip; production code always uses defaultGetConversationReplies.
var getConversationReplies = defaultGetConversationReplies

func defaultGetConversationReplies(sc *channels.SlackClient, params *slack.GetConversationRepliesParameters) ([]slack.Message, error) {
	var replies []slack.Message
	err := channels.WithSlackRetry(3, fmt.Sprintf("thread %s/%s", params.ChannelID, params.Timestamp), func() error {
		var e error
		replies, _, _, e = sc.GetAPI().GetConversationReplies(params)
		return e
	})
	return replies, err
}

// fetchAndDispatchThreadGroup fetches thread replies and runs the shared candidate
// collection + dispatch logic. Both the hot sweep (processSlackThreadGroup) and the
// cold reconciliation tier (processColdReconciliationGroup) call this so completion
// detection lives in exactly one place; only the post-fetch status bookkeeping differs.
func fetchAndDispatchThreadGroup(ctx context.Context, sc *channels.SlackClient, group []store.SlackThreadMeta, botID string, aliasCache map[string]slackThreadIdentity, wg *sync.WaitGroup, budget *threadReplyBudget) (threadScanResult, bool) {
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
	replies, err := getConversationReplies(sc, params)
	if err != nil {
		return threadScanResult{}, false
	}

	res := scanThreadReplies(replies, minLastTS, rep.LastActivityTS, botID)
	for _, sub := range group {
		ident, ok := aliasCache[sub.UserEmail]
		if !ok || ident.user == nil {
			continue
		}
		candidates := collectThreadCandidates(ctx, sc, ident.user, sub, replies, res, ident.effAliases, budget)
		if len(candidates) > 0 {
			analyzeSlackBatch(ctx, ident.user, sc, sub.ChannelID, candidates, wg)
		}
	}
	return res, true
}

func processSlackThreadGroup(ctx context.Context, sc *channels.SlackClient, group []store.SlackThreadMeta, botID string, aliasCache map[string]slackThreadIdentity, wg *sync.WaitGroup, budget *threadReplyBudget) {
	res, ok := fetchAndDispatchThreadGroup(ctx, sc, group, botID, aliasCache, wg, budget)
	if !ok {
		return
	}
	updateThreadStatusGroup(ctx, sc, group, res)
}

// processColdReconciliationGroup mirrors processSlackThreadGroup but must not reactivate
// slack_threads status or apply the 7-day timeout: a newly-resolved thread still gets
// closed (idempotent, status was already non-active) but an unresolved one only advances
// its reply cursor via TouchSlackThreadTimestamps, leaving status untouched.
func processColdReconciliationGroup(ctx context.Context, sc *channels.SlackClient, group []store.SlackThreadMeta, botID string, aliasCache map[string]slackThreadIdentity, wg *sync.WaitGroup, budget *threadReplyBudget) {
	res, ok := fetchAndDispatchThreadGroup(ctx, sc, group, botID, aliasCache, wg, budget)
	if !ok {
		return
	}
	if res.isResolved {
		updateThreadStatusGroup(ctx, sc, group, res)
		return
	}
	for _, s := range group {
		if res.newLastTS == s.LastTS && res.newLastActivity == s.LastActivityTS {
			continue
		}
		if err := store.TouchSlackThreadTimestamps(ctx, s.ChannelID, s.ThreadTS, res.newLastTS, res.newLastActivity, s.UserEmail); err != nil {
			logger.Warnf("[SLACK] cold tier: failed to advance cursor for %s/%s: %v", s.ChannelID, s.ThreadTS, err)
		}
	}
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

func collectThreadCandidates(ctx context.Context, sc *channels.SlackClient, user *store.User, t store.SlackThreadMeta, replies []slack.Message, res threadScanResult, effAl []string, budget *threadReplyBudget) []types.RawMessage {
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
		dispatchThreadCompletionIfMine(ctx, sc, user, t, m, budget)
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

// Why: the live-scan sibling (dispatchSlackThreadedCompletion) propagates the full
// envelope; the sweeper left Room empty and validateTargetTask rejects a blank Room
// as a cross-room operation, so every sweeper-side completion was dropped.
func buildThreadCompletionEnvelope(user *store.User, t store.SlackThreadMeta, m slack.Message, room, senderName string, fromMe bool) store.ConsolidatedMessage {
	ts := channels.ParseSlackTimestamp(m.Timestamp)
	env := store.ConsolidatedMessage{
		UserEmail: user.Email, Source: store.SourceSlack,
		Room:           room,
		Link:           buildSlackLink(types.RawMessage{ID: m.Timestamp, ChannelID: t.ChannelID, ReplyToID: t.ThreadTS}),
		Requester:      senderName,
		AssignedAt:     ts,
		CreatedAt:      ts,
		ThreadID:       t.ThreadTS,
		RepliedToID:    t.ThreadTS,
		OriginalText:   m.Text,
		SourceTS:       m.Timestamp,
		SourceChannels: []string{store.SourceSlack},
	}
	if fromMe {
		env.RequesterCanonical = user.Email
	}
	return env
}

func dispatchThreadCompletionIfMine(ctx context.Context, sc *channels.SlackClient, user *store.User, t store.SlackThreadMeta, m slack.Message, budget *threadReplyBudget) {
	if deps.completionSvc == nil || m.ThreadTimestamp == "" {
		return
	}
	senderName := sc.GetUserName(ctx, m.User)
	room := sc.GetChannelName(t.ChannelID)
	if strings.EqualFold(m.User, user.SlackID) || senderName == user.Name {
		env := buildThreadCompletionEnvelope(user, t, m, room, senderName, true)
		if _, err := deps.completionSvc.ProcessPotentialCompletion(ctx, env); err != nil {
			logger.Warnf("[SLACK] thread completion failed for %s: %v", user.Email, err)
		}
		return
	}
	dispatchCounterpartyThreadReply(ctx, user, t, m, room, senderName, budget)
}

// dispatchCounterpartyThreadReply handles a reply from someone other than the tracked
// user. Why: production evidence showed every real completion was a plain same-thread
// reply (an ack or an answer) that never used explicit completion wording, so the
// keyword gate below only ever saw the minority of cases. When the thread has an open
// task of its own, EvaluateThreadReply judges the reply directly, without the gate; the
// keyword-gated cross-channel path stays as the fallback for threads with no open task.
func dispatchCounterpartyThreadReply(ctx context.Context, user *store.User, t store.SlackThreadMeta, m slack.Message, room, senderName string, budget *threadReplyBudget) {
	env := buildThreadCompletionEnvelope(user, t, m, room, senderName, false)
	tasks, err := store.GetIncompleteByThreadID(ctx, store.GetDB(), user.Email, t.ThreadTS)
	if err != nil {
		logger.Warnf("[SLACK] thread reply task lookup failed for %s: %v", user.Email, err)
		return
	}
	if len(tasks) == 0 {
		if services.HasCompletionSignal(m.Text) {
			if _, err := deps.completionSvc.ProcessCrossChannelSignal(ctx, env); err != nil {
				logger.Warnf("[SLACK] thread cross-channel completion failed for %s: %v", user.Email, err)
			}
		}
		return
	}
	if !budget.take() {
		logger.Warnf("[SLACK] thread reply evaluation capped at %d for this sweep; skipping thread=%s", maxThreadReplyEvalsPerSweep, t.ThreadTS)
		return
	}
	if _, err := deps.completionSvc.EvaluateThreadReply(ctx, env, tasks); err != nil {
		logger.Warnf("[SLACK] thread reply evaluation failed for %s: %v", user.Email, err)
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
		closeResolvedThreadGroup(ctx, sc, group)
		return
	}
	for _, s := range group {
		updateThreadStatus(ctx, sc, s, res)
	}
}

// closeResolvedThreadGroup posts the resolution notice once for the group's
// representative thread, then proposes a completion candidate and closes
// tracking for every thread in the group.
func closeResolvedThreadGroup(ctx context.Context, sc *channels.SlackClient, group []store.SlackThreadMeta) {
	rep := group[0]
	if rep.ThreadTS == "" {
		logger.Warnf("[SLACK] updateThreadStatus: empty ThreadTS channel=%s, skipping PostMessage", rep.ChannelID)
	} else {
		msg := "This issue has been marked as resolved and monitoring is closed."
		if _, _, err := sc.GetAPI().PostMessage(rep.ChannelID, slack.MsgOptionText(msg, false), slack.MsgOptionTS(rep.ThreadTS)); err != nil {
			logger.Warnf("[SLACK] updateThreadStatus: PostMessage failed channel=%s thread=%s: %v", rep.ChannelID, rep.ThreadTS, err)
		}
	}
	for _, s := range group {
		proposeThreadCheckCompletion(ctx, s)
		_ = store.CloseTargetedThread(ctx, s.ChannelID, s.ThreadTS, s.UserEmail)
	}
}

// proposeThreadCheckCompletion records a confirm-first completion candidate for every
// still-open task in this thread when the thread's ✅ reaction closes tracking. Why: the
// channel gets told "resolved and monitoring is closed" but the task list does not move
// on its own — this surfaces a one-tap confirmation instead of the task silently going
// stale while the thread is no longer watched.
func proposeThreadCheckCompletion(ctx context.Context, t store.SlackThreadMeta) {
	if t.ThreadTS == "" {
		return
	}
	conn := store.GetDB()
	tasks, err := store.GetIncompleteByThreadID(ctx, conn, t.UserEmail, t.ThreadTS)
	if err != nil {
		logger.Warnf("[SLACK] proposeThreadCheckCompletion: lookup failed thread=%s user=%s: %v", t.ThreadTS, t.UserEmail, err)
		return
	}
	sourceKey := fmt.Sprintf("slack-check:%s:%s", t.ChannelID, t.ThreadTS)
	for _, task := range tasks {
		if store.WasCandidateDismissed(string(task.Metadata), sourceKey) {
			continue
		}
		cand := store.CompletionCandidate{
			SourceLink: sourceKey,
			SourceText: "✅",
			Evidence:   "✅ reaction on the thread",
			DetectedAt: time.Now().UTC().Format(time.RFC3339),
			Status:     "pending",
		}
		if err := store.AddCompletionCandidate(ctx, conn, t.UserEmail, task.ID, cand); err != nil {
			logger.Warnf("[SLACK] proposeThreadCheckCompletion: record candidate failed task=%d: %v", task.ID, err)
		}
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
