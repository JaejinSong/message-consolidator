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
		if isChannelInaccessible(slackClientKindForEmail(rep.UserEmail), rep.ChannelID) {
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
		if isChannelInaccessible(slackClientKindForEmail(rep.UserEmail), rep.ChannelID) {
			continue
		}
		processColdReconciliationGroup(ctx, sc, group, botID, aliasCache, wg, budget)
	}
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

	// Why: fetch with the group's own user's Slack token when one is on file, else the
	// bot client — same choice the live scanner makes in scanSlackForTokenUser.
	fetchClient := clientForSlackUser(ctx, rep.UserEmail, sc)
	params := &slack.GetConversationRepliesParameters{
		ChannelID: rep.ChannelID, Timestamp: rep.ThreadTS, Oldest: minLastTS, Limit: 100,
	}
	replies, err := getConversationReplies(fetchClient, params)
	if err != nil {
		return threadScanResult{}, false
	}

	res := scanThreadReplies(replies, minLastTS, rep.LastActivityTS, botID)
	for _, sub := range group {
		ident, ok := aliasCache[sub.UserEmail]
		if !ok || ident.user == nil {
			continue
		}
		candidates := collectThreadCandidates(ctx, fetchClient, ident.user, sub, replies, res, ident.effAliases, budget)
		if len(candidates) > 0 {
			analyzeSlackBatch(ctx, ident.user, fetchClient, sub.ChannelID, candidates, wg)
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
			if err := store.CloseTargetedThread(ctx, s.ChannelID, s.ThreadTS, s.UserEmail); err != nil {
				logger.Warnf("[SLACK] handleThreadTimeout: CloseTargetedThread failed channel=%s thread=%s user=%s: %v", s.ChannelID, s.ThreadTS, s.UserEmail, err)
			}
		}
		return
	}
	for _, s := range group {
		if err := store.CloseTargetedThread(ctx, s.ChannelID, s.ThreadTS, s.UserEmail); err != nil {
			logger.Warnf("[SLACK] handleThreadTimeout: CloseTargetedThread failed channel=%s thread=%s user=%s: %v", s.ChannelID, s.ThreadTS, s.UserEmail, err)
		}
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

func updateThreadStatus(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta, res threadScanResult) {
	if res.isResolved {
		if t.ThreadTS == "" {
			logger.Warnf("[SLACK] updateThreadStatus: empty ThreadTS channel=%s user=%s, skipping PostMessage", t.ChannelID, t.UserEmail)
		} else {
			msg := "This issue has been marked as resolved and monitoring is closed."
			_, _, _ = sc.GetAPI().PostMessage(t.ChannelID, slack.MsgOptionText(msg, false), slack.MsgOptionTS(t.ThreadTS))
		}
		if err := store.CloseTargetedThread(ctx, t.ChannelID, t.ThreadTS, t.UserEmail); err != nil {
			logger.Warnf("[SLACK] updateThreadStatus: CloseTargetedThread failed channel=%s thread=%s user=%s: %v", t.ChannelID, t.ThreadTS, t.UserEmail, err)
		}
		return
	}
	if res.newLastTS != t.LastTS || res.newLastActivity != t.LastActivityTS {
		if err := store.UpdateTargetedThread(ctx, t.ChannelID, t.ThreadTS, res.newLastTS, res.newLastActivity, t.UserEmail); err != nil {
			logger.Warnf("[SLACK] updateThreadStatus: UpdateTargetedThread failed channel=%s thread=%s user=%s: %v", t.ChannelID, t.ThreadTS, t.UserEmail, err)
		}
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
		if err := store.CloseTargetedThread(ctx, s.ChannelID, s.ThreadTS, s.UserEmail); err != nil {
			logger.Warnf("[SLACK] closeResolvedThreadGroup: CloseTargetedThread failed channel=%s thread=%s user=%s: %v", s.ChannelID, s.ThreadTS, s.UserEmail, err)
		}
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
