package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"message-consolidator/channels"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
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

// Why: per-user Slack OAuth grants each need their own SlackClient (own users.list /
// AuthTest identity), cached by a hash of the token rather than the plaintext so a raw
// token never sits in a map key visible via a debugger or accidental log of map keys.
var (
	userSlackClientMu    sync.Mutex
	userSlackClientCache = map[string]*channels.SlackClient{}
)

func hashSlackToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func getOrInitUserSlackClient(token string) *channels.SlackClient {
	key := hashSlackToken(token)
	userSlackClientMu.Lock()
	defer userSlackClientMu.Unlock()
	if c, ok := userSlackClientCache[key]; ok {
		return c
	}
	c := channels.NewSlackClient(token)
	userSlackClientCache[key] = c
	return c
}

// slackTokenRevokedReasons are slack-go error strings that mean a per-user OAuth grant
// is dead and should be dropped rather than retried next cycle.
var slackTokenRevokedReasons = []string{"invalid_auth", "token_revoked", "account_inactive", "not_authed"}

func isSlackTokenRevoked(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, reason := range slackTokenRevokedReasons {
		if strings.Contains(msg, reason) {
			return true
		}
	}
	return false
}

// slackClientKindBot/slackClientKindUser tag which Slack identity a fetch used, so the
// thread sweeper's channel-inaccessible backoff (scanner_slack_threads.go) can key on
// (kind, channelID) instead of channelID alone.
const (
	slackClientKindBot  = "bot"
	slackClientKindUser = "user"
)

// slackClientKindForEmail reports which client kind will scan/fetch for email, without
// constructing a client. Why: the thread sweeper's per-channel backoff must key on this
// so a bot 'not_in_channel' failure never blocks a user-token fetch on the same channel.
func slackClientKindForEmail(email string) string {
	if store.HasSlackUserToken(email) {
		return slackClientKindUser
	}
	return slackClientKindBot
}

// clientForSlackUser returns email's own Slack client when a user token is on file,
// else falls back to the bot client sc.
func clientForSlackUser(ctx context.Context, email string, sc *channels.SlackClient) *channels.SlackClient {
	tok, ok, err := store.GetSlackUserToken(ctx, email)
	if err != nil || !ok {
		return sc
	}
	return getOrInitUserSlackClient(tok.Token) //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.
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

func splitSlackUsersByToken(users []store.User) (tokenUsers, botUsers []store.User) {
	for _, u := range users {
		if store.HasSlackUserToken(u.Email) {
			tokenUsers = append(tokenUsers, u)
			continue
		}
		botUsers = append(botUsers, u)
	}
	return tokenUsers, botUsers
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

// scanSlackForTokenUser scans Slack using u's own OAuth grant: LookupChannels/
// GetMessages only see u's own channel memberships. Cursors reuse the same store keys
// as the bot path, so a user switching between token and bot scanning resumes from
// wherever the last pass (either kind) left off.
func scanSlackForTokenUser(ctx context.Context, u store.User, wg *sync.WaitGroup) {
	tok, ok, err := store.GetSlackUserToken(ctx, u.Email)
	if err != nil || !ok {
		logger.Warnf("[SLACK] user-token lookup failed for %s: %v", u.Email, err)
		return
	}
	sc := getOrInitUserSlackClient(tok.Token) //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.

	chans, _, err := sc.LookupChannels()
	if err != nil {
		if !handleSlackTokenFailure(ctx, u.Email, err) {
			logger.Warnf("[SLACK] user-token channel fetch failed for %s: %v", u.Email, err)
		}
		return
	}
	if len(chans) == 0 {
		return
	}

	users := []store.User{u}
	userAl := prepareSlackUserAliases(ctx, users)
	candidates, newTS, fetchOK, fetchErr := collectSlackHistory(ctx, users, chans, sc, userAl)
	tracker := newSlackHeldTracker()
	processSlackCandidates(ctx, users, sc, candidates, wg, tracker)
	applySlackScanResults(ctx, newTS, tracker)
	if fetchOK && ctx.Err() == nil {
		markSlackScanSuccess(users, tracker)
		return
	}
	handleSlackTokenFailure(ctx, u.Email, fetchErr)
}

// handleSlackTokenFailure deletes email's Slack user token and logs the fallback
// decision when err indicates the OAuth grant itself is dead (see
// slackTokenRevokedReasons); other errors are left for the caller to log and retry
// next cycle. Returns true when the token was dropped.
func handleSlackTokenFailure(ctx context.Context, email string, err error) bool {
	if !isSlackTokenRevoked(err) {
		return false
	}
	logger.Warnf("[SLACK] user token rejected for %s (%s); falling back to the bot", email, err)
	_ = store.DeleteSlackUserToken(ctx, email)
	return true
}

// markSlackScanSuccess stamps the wall-clock time of the last clean scan pass, mirroring
// channels.markGmailScanSuccess. Why: /slack/status must be able to tell a live scan loop
// from a silently dead one (bot removed from channels, channel_not_found, etc). A user
// with any held cursor (tracker) is skipped -- their scan was not actually clean even
// though the channel fetch itself succeeded.
func markSlackScanSuccess(users []store.User, tracker *slackHeldTracker) {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	for _, u := range users {
		if tracker.hasHeld(u.Email) {
			logger.Warnf("[SLACK] withholding last_success stamp for %s: cursor(s) held", u.Email)
			continue
		}
		if err := store.UpdateLastScan(u.Email, store.SourceSlack, store.ScanTargetLastSuccess, ts); err != nil {
			logger.Warnf("[SLACK] record last_success failed for %s: %v", u.Email, err)
		}
	}
}

// slackHeldTracker collects the (email, channelID) pairs whose AI analysis did not
// finish this scan pass (AckScanned ok=false), so applySlackScanResults can withhold
// their cursor advance instead of skipping past the unanalyzed backlog.
type slackHeldTracker struct {
	mu   sync.Mutex
	held map[string]map[string]bool // email -> channelID -> held
}

func newSlackHeldTracker() *slackHeldTracker {
	return &slackHeldTracker{held: make(map[string]map[string]bool)}
}

func (t *slackHeldTracker) hold(email, channelID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.held[email] == nil {
		t.held[email] = make(map[string]bool)
	}
	t.held[email][channelID] = true
}

func (t *slackHeldTracker) isHeld(email, channelID string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.held[email][channelID]
}

// hasHeld reports whether email has any held channel this pass.
func (t *slackHeldTracker) hasHeld(email string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.held[email]) > 0
}

// applySlackScanResults commits newTS to store.UpdateLastScan, withholding any
// (email, channelID) tracker marked held and -- per the incident fix -- the entire
// pass when ctx is already done: a scan that timed out mid-analysis must not advance
// past whatever channels happened to fetch before the deadline hit.
func applySlackScanResults(ctx context.Context, newTS map[string]map[string]string, tracker *slackHeldTracker) {
	if err := ctx.Err(); err != nil {
		logger.Warnf("[SLACK] ctx done (%v); holding all cursors this pass", err)
		return
	}
	for email, channelMap := range newTS {
		for chanID := range channelMap {
			if tracker.isHeld(email, chanID) {
				logger.Warnf("[SLACK] holding cursor for %s/%s: analysis incomplete", email, chanID)
				delete(channelMap, chanID)
			}
		}
	}
	updateSlackCursors(newTS)
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

func updateChannelCursor(newTS map[string]map[string]string, email, channelID, msgID string) {
	if newTS[email] == nil {
		newTS[email] = make(map[string]string)
	}
	if curr, ok := newTS[email][channelID]; !ok || msgID > curr {
		newTS[email][channelID] = msgID
	}
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

// slackAdapter feeds one user's per-channel candidate batches (already
// classified in the drain phase) through the shared channel driver.
type slackAdapter struct {
	// ctx is the scan-scoped context; BuildPayload/Mentions resolve user names
	// through the Slack API and the ChannelAdapter interface carries no ctx.
	ctx      context.Context
	sc       *channels.SlackClient
	buf      map[string][]types.RawMessage // channelName → msgs
	rooms    map[string]string             // channelName → channelID
	chanOf   map[string]string             // messageID → channelID, for AckScanned
	tracker  *slackHeldTracker
	consumed bool
}

func newSlackAdapter(ctx context.Context, sc *channels.SlackClient, byChannel map[string][]types.RawMessage, tracker *slackHeldTracker) *slackAdapter {
	buf := make(map[string][]types.RawMessage, len(byChannel))
	rooms := make(map[string]string, len(byChannel))
	chanOf := make(map[string]string, len(byChannel))
	for channelID, msgs := range byChannel {
		name := sc.GetChannelName(channelID)
		buf[name] = append(buf[name], msgs...)
		rooms[name] = channelID
		for _, m := range msgs {
			chanOf[m.ID] = channelID
		}
	}
	return &slackAdapter{ctx: ctx, sc: sc, buf: buf, rooms: rooms, chanOf: chanOf, tracker: tracker}
}

// AckScanned records which channelID a failed group's messages belong to, via
// slackHeldTracker, so scanSlack can withhold that channel's cursor advance instead
// of masking unanalyzed history as processed. ok=true is a no-op: the drain-phase
// cursor advance in classifyAndCollect already covers this case.
func (a *slackAdapter) AckScanned(_ context.Context, email string, ids []string, ok bool) {
	if ok || a.tracker == nil {
		return
	}
	for _, id := range ids {
		if chanID, found := a.chanOf[id]; found {
			a.tracker.hold(email, chanID)
		}
	}
}

func (a *slackAdapter) Source() string    { return store.SourceSlack }
func (a *slackAdapter) LogPrefix() string { return "SLACK" }

func (a *slackAdapter) PopMessages(string) map[string][]types.RawMessage {
	if a.consumed {
		return nil
	}
	a.consumed = true
	return a.buf
}

func (a *slackAdapter) GetGroupName(_, roomKey string) string { return roomKey }

// Is1To1 — Slack DM channel IDs carry the "D" prefix.
func (a *slackAdapter) Is1To1(roomKey string) bool {
	return strings.HasPrefix(a.rooms[roomKey], "D")
}

func (a *slackAdapter) BuildPayload(_ store.User, _ []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	return buildSlackAnalysisPayload(a.ctx, msgs, a.sc)
}

func (a *slackAdapter) Enrich(roomKey, payload string, ts time.Time) (*types.EnrichedMessage, error) {
	var last types.RawMessage
	if msgs := a.buf[roomKey]; len(msgs) > 0 {
		last = msgs[len(msgs)-1]
	}
	senderName := last.SenderName
	if senderName == "" {
		senderName = last.Sender
	}
	return EnrichSlackMessage(last.Sender, senderName, last.ChannelID, last.ReplyToID, payload, ts)
}

// IsFromMe — always false toward the driver: Slack owns its fromMe behaviors in
// the drain phase (completion dispatch in classifyAndCollect) and historically
// never applied the driver's fromMe-in-group category override.
func (a *slackAdapter) IsFromMe(types.RawMessage, store.User) bool { return false }

// IsOwnMessage — the real sender-identity check, kept separate from IsFromMe so
// the category-override path (saveChannelItem) stays false while the
// resolve-trust path (candidate injection) still sees the user's own messages.
func (a *slackAdapter) IsOwnMessage(m types.RawMessage, user store.User) bool {
	return isFromUser(&user, m)
}

func (a *slackAdapter) Mentions(m types.RawMessage) []string {
	return resolveSlackMentionNames(a.ctx, a.sc, extractSlackMentionUserIDs(m.Text))
}

// ownsCompletionDispatch — dispatchOutgoingCompletionIfMine already covers ALL
// raw rows pre-classification; the driver must not re-dispatch the classified subset.
func (a *slackAdapter) ownsCompletionDispatch() {}

// SaveThreadID — replies anchor on the parent thread ts, root messages on their own ID.
func (a *slackAdapter) SaveThreadID(m types.RawMessage) string { return slackThreadTS(m) }

// ProposalThreadID — Slack RawMessages never populate ThreadID (only ReplyToID on
// replies), so the candidate-injection loop needs this to guard against
// cross-thread fuzzy matches. Uses the same anchor as SaveThreadID so a proposal's
// thread lines up with the thread_id already persisted on the task it may match.
func (a *slackAdapter) ProposalThreadID(m types.RawMessage) string { return slackThreadTS(m) }

func (a *slackAdapter) SaveLink(ctx context.Context, m types.RawMessage, email string) string {
	return buildSlackLinkAndRegisterThread(ctx, m, email)
}

func updateSlackCursors(newTS map[string]map[string]string) {
	for email, channelMap := range newTS {
		for chanID, ts := range channelMap {
			if err := store.UpdateLastScan(email, store.SourceSlack, chanID, ts); err != nil {
				logger.Warnf("[SCAN] slack: UpdateLastScan failed for %s/%s: %v", email, chanID, err)
			}
		}
	}
}

func buildSlackAnalysisPayload(ctx context.Context, candidates []types.RawMessage, sc slackUserResolver) (string, map[string]types.RawMessage) {
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
		tsTag := formatTSTag(m.Timestamp)
		writePayloadLine(&sb, m.ID, tsTag, metaStr, senderLabel, resolvedText)
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
	return formatTagBlock(tags) +
		formatListBlock("Reactions", m.Reactions) +
		formatListBlock("Files", m.AttachmentNames)
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
