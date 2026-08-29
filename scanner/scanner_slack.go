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

// collectSlackHistory returns candidates keyed email → channelID → messages so
// each channel is analyzed as its own room (mixing channels into one batch keyed
// by the first message's channel produced wrong Room values).
func collectSlackHistory(ctx context.Context, users []store.User, chans []slack.Channel, sc *channels.SlackClient, userAl map[string][]string) (map[string]map[string][]types.RawMessage, map[string]map[string]string) {
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
	_ = eg.Wait()
	return candidates, newTS
}

func scanSingleSlackChannel(ctx context.Context, users []store.User, c slack.Channel, sc *channels.SlackClient, userAl map[string][]string, mu *sync.Mutex, candidates map[string]map[string][]types.RawMessage, newTS map[string]map[string]string) error {
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

// Why: When the user replies in their own thread we evaluate state (RESOLVE/UPDATE) on a Background ctx so Gemini latency doesn't block the scan loop.
// _ ctx is accepted for trace propagation parity with the rest of classifyAndCollect; the goroutine itself uses Background.
func dispatchOutgoingCompletionIfMine(_ context.Context, sc *channels.SlackClient, u store.User, m types.RawMessage) {
	if deps.completionSvc == nil {
		return
	}
	if m.ReplyToID != "" && isFromUser(&u, m) {
		dispatchSlackThreadedCompletion(sc, u, m) //nolint:contextcheck // dispatch spawns a goroutine that outlives the scan ctx by design
		return
	}
	// Why: sibling path for plain (non-reply) messages carrying a completion signal —
	// confirm-first cross-channel candidate matching, never auto-closes.
	if services.HasCompletionSignal(m.Text) {
		dispatchSlackCrossChannelCompletion(sc, u, m) //nolint:contextcheck // dispatch spawns a goroutine that outlives the scan ctx by design
	}
}

// dispatchSlackThreadedCompletion handles a fromMe quoted reply — same-thread
// completion/update signal. Why: completion-fallback may INSERT a new task when
// the thread has no open parent. Propagate envelope (Requester/Room/Link/
// AssignedAt/SourceChannels) so the resulting row matches the normal scanner path
// instead of empty fields.
func dispatchSlackThreadedCompletion(sc *channels.SlackClient, u store.User, m types.RawMessage) {
	room := sc.GetChannelName(m.ChannelID)
	link := buildSlackLink(m)
	go func(bgCtx context.Context, email string, raw types.RawMessage, room, link string) { // Why: goroutine outlives the parent scan ctx by design; bgCtx is passed in explicitly.
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
	}(context.Background(), u.Email, m, room, link)
}

// dispatchSlackCrossChannelCompletion feeds a signal-bearing non-reply message
// (from the user or a counterparty) to the confirm-first cross-channel pipeline.
func dispatchSlackCrossChannelCompletion(sc *channels.SlackClient, u store.User, m types.RawMessage) {
	room := sc.GetChannelName(m.ChannelID)
	link := buildSlackLink(m)
	fromMe := isFromUser(&u, m)
	go func(bgCtx context.Context, email string, raw types.RawMessage, room, link string, fromMe bool) { // Why: goroutine outlives the parent scan ctx by design; bgCtx is passed in explicitly.
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
	}(context.Background(), u.Email, m, room, link, fromMe)
}

func updateChannelCursor(newTS map[string]map[string]string, email, channelID, msgID string) {
	if newTS[email] == nil {
		newTS[email] = make(map[string]string)
	}
	if curr, ok := newTS[email][channelID]; !ok || msgID > curr {
		newTS[email][channelID] = msgID
	}
}

func processSlackCandidates(ctx context.Context, users []store.User, sc *channels.SlackClient, candidates map[string]map[string][]types.RawMessage, wg *sync.WaitGroup) {
	for email, byChannel := range candidates {
		user, err := store.GetOrCreateUser(ctx, email, "", "")
		if err != nil || user == nil {
			continue
		}
		aliases, _ := store.GetUserAliases(ctx, user.ID)
		logger.Debugf("[SLACK] user %s: %d channels queued for AI analysis", email, len(byChannel))
		scanChannel(ctx, *user, aliases, "Korean", wg, newSlackAdapter(ctx, sc, byChannel))
	}
}

// analyzeSlackBatch runs one channel's classified candidates through the shared
// driver — the thread sweeper's entry point into the same pipeline.
func analyzeSlackBatch(ctx context.Context, user *store.User, sc *channels.SlackClient, channelID string, candidates []types.RawMessage, wg *sync.WaitGroup) {
	if len(candidates) == 0 {
		return
	}
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	byChannel := map[string][]types.RawMessage{channelID: candidates}
	scanChannel(ctx, *user, aliases, "Korean", wg, newSlackAdapter(ctx, sc, byChannel))
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
	consumed bool
}

func newSlackAdapter(ctx context.Context, sc *channels.SlackClient, byChannel map[string][]types.RawMessage) *slackAdapter {
	buf := make(map[string][]types.RawMessage, len(byChannel))
	rooms := make(map[string]string, len(byChannel))
	for channelID, msgs := range byChannel {
		name := sc.GetChannelName(channelID)
		buf[name] = append(buf[name], msgs...)
		rooms[name] = channelID
	}
	return &slackAdapter{ctx: ctx, sc: sc, buf: buf, rooms: rooms}
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

func (a *slackAdapter) Mentions(m types.RawMessage) []string {
	return resolveSlackMentionNames(a.ctx, a.sc, extractSlackMentionUserIDs(m.Text))
}

// ownsCompletionDispatch — dispatchOutgoingCompletionIfMine already covers ALL
// raw rows pre-classification; the driver must not re-dispatch the classified subset.
func (a *slackAdapter) ownsCompletionDispatch() {}

// SaveThreadID — replies anchor on the parent thread ts, root messages on their own ID.
func (a *slackAdapter) SaveThreadID(m types.RawMessage) string { return slackThreadTS(m) }

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
		var tsTag string
		if !m.Timestamp.IsZero() {
			tsTag = fmt.Sprintf("[ts:%s]", m.Timestamp.UTC().Format("2006-01-02T15:04"))
		}
		fmt.Fprintf(&sb, "[ID:%s]%s%s %s: %s\n", m.ID, tsTag, metaStr, senderLabel, resolvedText)
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
		fmt.Fprintf(&sb, " [Tags: %s]", strings.Join(tags, ", "))
	}
	if len(m.Reactions) > 0 {
		fmt.Fprintf(&sb, " [Reactions: %s]", strings.Join(m.Reactions, ", "))
	}
	if len(m.AttachmentNames) > 0 {
		fmt.Fprintf(&sb, " [Files: %s]", strings.Join(m.AttachmentNames, ", "))
	}
	return sb.String()
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
