package scanner

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
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

// scanSlack splits users into those with their own Slack OAuth grant (tokenUsers) and
// everyone else (botUsers). tokenUsers are scanned with their own client so they see
// exactly their own channel memberships; botUsers keep the original shared-bot path.
// Why: a channel visible to both a tokenUser and the bot must only be processed once
// per user, so botUsers explicitly excludes anyone already covered by their own token.
func scanSlack(ctx context.Context, users []store.User, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" || len(users) == 0 {
		return
	}
	tokenUsers, botUsers := splitSlackUsersByToken(users)
	for _, u := range tokenUsers {
		scanSlackForTokenUser(ctx, u, wg)
	}
	if len(botUsers) > 0 {
		scanSlackWithBot(ctx, botUsers, wg)
	}

	//Why: Forces immediate persistence of scan cursors after each cycle to prevent data loss or scan gaps in case of process termination.
	for _, u := range users {
		store.PersistAllScanMetadata(ctx, u.Email)
	}
}

// scanSlackWithBot is the original shared-bot scan path, now run only against users
// that have no Slack OAuth grant of their own.
func scanSlackWithBot(ctx context.Context, users []store.User, wg *sync.WaitGroup) {
	sc, botID := getOrInitSlackClient(cfg.SlackToken) //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.

	chans, _, err := sc.LookupChannels()
	if err != nil {
		logger.Errorf("[SCAN] slack: failed to fetch channels: %v", err)
		return
	}
	if len(chans) == 0 {
		warnSlackNoChannels(ctx, sc, botID)
		return
	}

	userAl := prepareSlackUserAliases(ctx, users)
	candidates, newTS, fetchOK, _ := collectSlackHistory(ctx, users, chans, sc, userAl)
	tracker := newSlackHeldTracker()
	processSlackCandidates(ctx, users, sc, candidates, wg, tracker)
	applySlackScanResults(ctx, newTS, tracker)

	// Why: a clean pass (no channel-level fetch errors) is the same signal Gmail uses
	// to distinguish "no new messages" from "scan silently failed" — 2026-09-17 the bot
	// was removed from every channel for 10 days while /slack/status stayed green.
	if fetchOK && ctx.Err() == nil {
		markSlackScanSuccess(users, tracker)
	}
}

// Why: prime interval (59m) rate-limits the "bot removed from every channel" error to
// once per hour so a stuck bot doesn't flood the log across every scan cycle.
const slackNoChannelsLogInterval = 59 * time.Minute

var (
	slackNoChannelsLogMu    sync.Mutex
	slackNoChannelsLoggedAt time.Time
)

// warnSlackNoChannels logs the zero-membership condition at most once per interval.
// Why: users.conversations returning [] (not an error) is exactly the 2026-09-17
// incident shape — the bot was kicked from every channel and scanning stopped silently.
func warnSlackNoChannels(ctx context.Context, sc *channels.SlackClient, botID string) {
	slackNoChannelsLogMu.Lock()
	defer slackNoChannelsLogMu.Unlock()
	if time.Since(slackNoChannelsLoggedAt) < slackNoChannelsLogInterval {
		return
	}
	slackNoChannelsLoggedAt = time.Now()
	botName := sc.GetUserName(ctx, botID)
	if botName == "" {
		botName = botID
	}
	logger.Errorf("[SLACK] bot is not a member of any channel — invite @%s to resume scanning", botName)
}

func prepareSlackUserAliases(ctx context.Context, users []store.User) map[string][]string {
	ua := make(map[string][]string)
	for _, u := range users {
		aliases, _ := store.GetUserAliases(ctx, u.ID)
		ua[u.Email] = services.GetEffectiveAliases(u, aliases)
	}
	return ua
}

// collectSlackHistory returns candidates keyed email → channelID → messages so
// each channel is analyzed as its own room (mixing channels into one batch keyed
// by the first message's channel produced wrong Room values). The bool return is
// fetchOK — false when any channel's GetMessages failed, so callers can withhold
// the last_success stamp instead of masking a partial fetch as a clean pass. The
// error return is the first channel fetch failure (nil when fetchOK), so a
// user-token caller can tell a revoked grant apart from a transient failure.
func collectSlackHistory(ctx context.Context, users []store.User, chans []slack.Channel, sc *channels.SlackClient, userAl map[string][]string) (map[string]map[string][]types.RawMessage, map[string]map[string]string, bool, error) {
	candidates := make(map[string]map[string][]types.RawMessage)
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
	fetchErr := eg.Wait()
	return candidates, newTS, fetchErr == nil, fetchErr
}

func scanSingleSlackChannel(ctx context.Context, users []store.User, c slack.Channel, sc *channels.SlackClient, userAl map[string][]string, mu *sync.Mutex, candidates map[string]map[string][]types.RawMessage, newTS map[string]map[string]string) error {
	minTS := getMinLastTS(users, c.ID)
	logger.Debugf("[SLACK] channel %s: minTS=%s", c.ID, minTS)
	since := slackScanWindow(minTS, time.Now())
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

const (
	slackDefaultLookback = 24 * time.Hour
	// slackCatchUpCap bounds a resumed scan. Why: a bot-removal outage can outlast
	// a week (2026-09-17, 10 days) and these channels carry low history volume, so
	// 29 days (prime) catches up without risking the 60s scan timeout.
	slackCatchUpCap = 29 * 24 * time.Hour
)

// slackScanWindow reports the oldest message a scan should process. Why: `since` used
// to be a flat now-24h while the comment claimed minTS widened it, so any gap longer
// than a day was skipped outright — processHistoryMessages drops anything older and
// stops paginating, and the cursor then advances past the island. The cursor now widens
// the window, capped, and never narrows it below the default lookback.
func slackScanWindow(minTS string, now time.Time) time.Time {
	def := now.Add(-slackDefaultLookback)
	if minTS == "" {
		return def
	}
	cursor := channels.ParseSlackTimestamp(minTS)
	if cursor.IsZero() || cursor.Unix() <= 0 || cursor.After(def) {
		return def
	}
	if floor := now.Add(-slackCatchUpCap); cursor.Before(floor) {
		return floor
	}
	return cursor
}

func getMinLastTS(users []store.User, channelID string) string {
	min := ""
	for _, u := range users {
		ts := store.GetLastScan(u.Email, store.SourceSlack, channelID)
		if ts == "" {
			return ""
		}
		if min == "" || ts < min {
			min = ts
		}
	}
	return min
}

func classifyAndCollect(ctx context.Context, c slack.Channel, sc *channels.SlackClient, m types.RawMessage, users []store.User, userAl map[string][]string, candidates map[string]map[string][]types.RawMessage, newTS map[string]map[string]string) {
	m.ChannelID = c.ID
	for _, u := range users {
		lts := store.GetLastScan(u.Email, store.SourceSlack, c.ID)
		if lts != "" && m.ID <= lts {
			continue
		}
		dispatchOutgoingCompletionIfMine(ctx, sc, u, m)
		cls := classifyMessage(c, &u, userAl[u.Email], m)
		if cls == types.CategoryTask || cls == types.CategoryQuery {
			if candidates[u.Email] == nil {
				candidates[u.Email] = make(map[string][]types.RawMessage)
			}
			candidates[u.Email][c.ID] = append(candidates[u.Email][c.ID], m)
		}
		updateChannelCursor(newTS, u.Email, c.ID, m.ID)
	}
}

// slackCompletionDispatchTimeout bounds a detached completion-dispatch goroutine so it
// cannot run forever once cancellation is stripped from its ctx. Why: prime, matches the
// scan loop's own budget (see scanSlack timeout) as the upper bound for outlived work.
const slackCompletionDispatchTimeout = 293 * time.Second

// detachedDispatchCtx derives a ctx for a completion-dispatch goroutine that must outlive
// the parent scan ctx: WithoutCancel keeps the WhaTap trace (carried as a value) while
// dropping the parent's cancellation, and WithTimeout re-adds an upper bound so a detached
// goroutine cannot run forever.
func detachedDispatchCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), slackCompletionDispatchTimeout)
}

// Why: When the user replies in their own thread we evaluate state (RESOLVE/UPDATE) on a
// detached ctx (see detachedDispatchCtx) so Gemini latency doesn't block the scan loop,
// while still carrying the scan's WhaTap trace instead of a bare context.Background().
func dispatchOutgoingCompletionIfMine(ctx context.Context, sc *channels.SlackClient, u store.User, m types.RawMessage) {
	if deps.completionSvc == nil {
		return
	}
	if m.ReplyToID != "" && isFromUser(&u, m) {
		dispatchSlackThreadedCompletion(ctx, sc, u, m)
		return
	}
	// Why: sibling path for plain (non-reply) messages carrying a completion signal —
	// confirm-first cross-channel candidate matching, never auto-closes.
	if services.HasCompletionSignal(m.Text) {
		dispatchSlackCrossChannelCompletion(ctx, sc, u, m)
	}
}

// dispatchSlackThreadedCompletion handles a fromMe quoted reply — same-thread
// completion/update signal. Why: completion-fallback may INSERT a new task when
// the thread has no open parent. Propagate envelope (Requester/Room/Link/
// AssignedAt/SourceChannels) so the resulting row matches the normal scanner path
// instead of empty fields.
func dispatchSlackThreadedCompletion(ctx context.Context, sc *channels.SlackClient, u store.User, m types.RawMessage) {
	room := sc.GetChannelName(m.ChannelID)
	link := buildSlackLink(m)
	dispatchCtx, cancel := detachedDispatchCtx(ctx)
	go func(bgCtx context.Context, cancel context.CancelFunc, email string, raw types.RawMessage, room, link string) { // Why: goroutine outlives the parent scan ctx by design; bgCtx is passed in explicitly.
		defer cancel()
		defer safego.Recover("slack-outgoing-completion")
		if _, err := deps.completionSvc.ProcessPotentialCompletion(bgCtx, store.ConsolidatedMessage{
			UserEmail: email, Source: store.SourceSlack,
			Room: room, Link: link,
			Requester: raw.Sender, RequesterCanonical: email,
			AssignedAt: raw.Timestamp, CreatedAt: raw.Timestamp,
			ThreadID: raw.ReplyToID, RepliedToID: raw.ReplyToID,
			OriginalText: raw.Text, SourceTS: raw.ID,
			SourceChannels: []string{store.SourceSlack},
		}); err != nil {
			logger.Warnf("[SLACK] outgoing completion failed for %s: %v", email, err)
		}
	}(dispatchCtx, cancel, u.Email, m, room, link)
}

// dispatchSlackCrossChannelCompletion feeds a signal-bearing non-reply message
// (from the user or a counterparty) to the confirm-first cross-channel pipeline.
func dispatchSlackCrossChannelCompletion(ctx context.Context, sc *channels.SlackClient, u store.User, m types.RawMessage) {
	room := sc.GetChannelName(m.ChannelID)
	link := buildSlackLink(m)
	fromMe := isFromUser(&u, m)
	dispatchCtx, cancel := detachedDispatchCtx(ctx)
	go func(bgCtx context.Context, cancel context.CancelFunc, email string, raw types.RawMessage, room, link string, fromMe bool) { // Why: goroutine outlives the parent scan ctx by design; bgCtx is passed in explicitly.
		defer cancel()
		defer safego.Recover("slack-crosschannel-completion")
		env := store.ConsolidatedMessage{
			UserEmail: email, Source: store.SourceSlack,
			Room: room, Link: link,
			Requester:      raw.Sender,
			AssignedAt:     raw.Timestamp,
			CreatedAt:      raw.Timestamp,
			ThreadID:       raw.ReplyToID,
			RepliedToID:    raw.ReplyToID,
			OriginalText:   raw.Text,
			SourceTS:       raw.ID,
			SourceChannels: []string{store.SourceSlack},
		}
		if fromMe {
			env.RequesterCanonical = email
		}
		if _, err := deps.completionSvc.ProcessCrossChannelSignal(bgCtx, env); err != nil {
			logger.Warnf("[SLACK] cross-channel completion failed for %s: %v", email, err)
		}
	}(dispatchCtx, cancel, u.Email, m, room, link, fromMe)
}

func processSlackCandidates(ctx context.Context, users []store.User, sc *channels.SlackClient, candidates map[string]map[string][]types.RawMessage, wg *sync.WaitGroup, tracker *slackHeldTracker) {
	for email, byChannel := range candidates {
		user, err := store.GetOrCreateUser(ctx, email, "", "")
		if err != nil || user == nil {
			continue
		}
		aliases, _ := store.GetUserAliases(ctx, user.ID)
		logger.Debugf("[SLACK] user %s: %d channels queued for AI analysis", email, len(byChannel))
		scanChannel(ctx, *user, aliases, "Korean", wg, newSlackAdapter(ctx, sc, byChannel, tracker))
	}
}

// analyzeSlackBatch runs one channel's classified candidates through the shared
// driver — the thread sweeper's entry point into the same pipeline. Why nil tracker:
// the sweeper does not own scanSlack's per-pass cursor bookkeeping, so there is
// nothing to withhold on an AckScanned failure here.
func analyzeSlackBatch(ctx context.Context, user *store.User, sc *channels.SlackClient, channelID string, candidates []types.RawMessage, wg *sync.WaitGroup) {
	if len(candidates) == 0 {
		return
	}
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	byChannel := map[string][]types.RawMessage{channelID: candidates}
	scanChannel(ctx, *user, aliases, "Korean", wg, newSlackAdapter(ctx, sc, byChannel, nil))
}
