package scanner

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/channels"
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
	"github.com/whatap/go-api/trace"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)


var slackMentionRegex = regexp.MustCompile(`<@([A-Z0-9]+)>`)

// Why: Tier 3 conversations.replies caps at ~50/min (=1.2s); 1.0s = 60/min is within burst tolerance,
// and `withSlackRetry` honors `Retry-After` if Slack pushes back. Saves ~200ms per thread iteration.
const SlackThrottlingInterval = 1000 * time.Millisecond

var slackLimiter = rate.NewLimiter(rate.Every(SlackThrottlingInterval), 1)

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
	GetUserName(userID string) string
}

func resolveSlackMentions(text string, sc slackUserResolver) string {
	return slackMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		userID := match[2 : len(match)-1]
		userName := sc.GetUserName(userID)
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
	sc := channels.NewSlackClient(cfg.SlackToken)
	_ = sc.FetchUsers()

	chans, _, err := sc.LookupChannels()
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch channels: %v", err)
		return
	}

	userAl := prepareSlackUserAliases(ctx, users)
	candidates, newTS := collectSlackHistory(ctx, users, chans, sc, userAl)
	processSlackCandidates(ctx, users, sc, candidates, wg)
	updateSlackCursors(newTS)

	//Why: Forces immediate persistence of scan cursors after each cycle to prevent data loss or scan gaps in case of process termination.
	for _, u := range users {
		store.PersistAllScanMetadata(u.Email)
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
	logger.Debugf("[SLACK-DEBUG] Channel %s: minTS=%s", c.ID, minTS)
	//Why: Uses a dual-strategy scan window. It scans up to 24 hours back by default, 
	// but respects minTS as a lower bound only if it provides a safer (older) starting point, 
	// preventing "islands" of unproccessed messages between scan intervals.
	since := time.Now().Add(-24 * time.Hour)
	msgs, err := sc.GetMessages(c.ID, since, minTS)
	if err != nil {
		logger.Errorf("[SLACK-ERROR] GetMessages failed for %s: %v", c.ID, err)
		return err
	}
	if len(msgs) == 0 {
		logger.Debugf("[SLACK-DEBUG] No new messages for channel %s (minTS: %s)", c.ID, minTS)
		return nil
	}

	mu.Lock()
	defer mu.Unlock()
	for _, m := range msgs {
		classifyAndCollect(ctx, c, m, users, userAl, candidates, newTS)
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

func classifyAndCollect(ctx context.Context, c slack.Channel, m types.RawMessage, users []store.User, userAl map[string][]string, candidates map[string][]types.RawMessage, newTS map[string]map[string]string) {
	for _, u := range users {
		lts := store.GetLastScan(u.Email, "slack", c.ID)
		if lts != "" && m.ID <= lts {
			continue
		}
		isFromMe := strings.EqualFold(m.Sender, u.Name) || strings.EqualFold(m.Sender, u.Email)
		if isFromMe && completionSvc != nil && m.ReplyToID != "" {
			// Why: [Async Transition] Triggers task state evaluation (RESOLVE/UPDATE) in a background goroutine 
			// to prevent Gemini API latency from blocking the ingestion loop. 
			// Uses context.Background() to decouple from parent scan timeout.
			go func(bgCtx context.Context, email string, raw types.RawMessage) {
				completionSvc.ProcessPotentialCompletion(bgCtx, store.ConsolidatedMessage{
					UserEmail: email, Source: "slack", ThreadID: raw.ReplyToID, 
					OriginalText: raw.Text, SourceTS: raw.ID, CreatedAt: raw.Timestamp,
				})
			}(context.Background(), u.Email, m)
		}
		cls := classifyMessage(c, &u, userAl[u.Email], m)
		// Soft Filtering: Always include Task and Query categories if they reached this stage.
		if cls == types.CategoryTask || cls == types.CategoryQuery {
			m.ChannelID = c.ID
			candidates[u.Email] = append(candidates[u.Email], m)
		}
		if newTS[u.Email] == nil {
			newTS[u.Email] = make(map[string]string)
		}
		if curr, ok := newTS[u.Email][c.ID]; !ok || m.ID > curr {
			newTS[u.Email][c.ID] = m.ID
		}
	}
}

func processSlackCandidates(ctx context.Context, users []store.User, sc *channels.SlackClient, candidates map[string][]types.RawMessage, wg *sync.WaitGroup) {
	for email, msgs := range candidates {
		user, err := store.GetOrCreateUser(ctx, email, "", "")
		if err != nil || user == nil {
			continue
		}
		// Why: Provides visibility into the collection pipeline by logging the number of candidates queued for AI analysis.
		logger.Debugf("[SLACK-COLLECT] User: %s, Total candidates for AI processing: %d", email, len(msgs))
		analyzeAndSaveSlack(ctx, user, sc, msgs, wg)
	}
}

func updateSlackCursors(newTS map[string]map[string]string) {
	for email, channelMap := range newTS {
		for chanID, ts := range channelMap {
			store.UpdateLastScan(email, "slack", chanID, ts)
		}
	}
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m types.RawMessage) types.MessageCategory {
	isFromMe := strings.EqualFold(m.Sender, user.Name) || strings.EqualFold(m.Sender, user.Email) || (user.SlackID != "" && m.Sender == user.SlackID)
	// Why: Identifies self-sent requests to others in non-direct channels to track "waiting for reply" states.
	if isFromMe && !channel.IsIM && !channel.IsMpIM {
		if strings.Contains(m.Text, "<@U") && (user.SlackID == "" || !strings.Contains(m.Text, "<@"+user.SlackID+">")) {
			return types.CategoryTask
		}
	}
	// Why: Broadens capture by prioritizing direct messages, group mentions, and specific personal mentions.
	if channel.IsIM || channel.IsMpIM || isGroupMention(m.Text) {
		return types.CategoryTask
	}
	if (user.SlackID != "" && strings.Contains(m.Text, "<@"+user.SlackID+">")) || hasAliasMatch(m, aliases) {
		return types.CategoryTask
	}
	return types.CategoryQuery
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

func startSlowSweeper(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	logger.Infof("Slack Slow Sweeper started (monitoring old threads)...")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepSlackThreads(ctx, wg)
		}
	}
}

func sweepSlackThreads(ctx context.Context, wg *sync.WaitGroup) {
	traceCtx, _ := trace.Start(ctx, "/Background-SweepSlackThreads")
	defer trace.End(traceCtx, nil)

	if cfg == nil || cfg.SlackToken == "" {
		return
	}

	threads, err := store.GetTargetedActiveThreads(traceCtx)
	if err != nil || len(threads) == 0 {
		return
	}

	sc, botID := getOrInitSlackClient(cfg.SlackToken)

	// Why: alias 결정은 tenant(UserEmail) 단위로 동일하므로 thread 루프 밖에서 1회만 조회한다.
	// 기존엔 thread당 GetUserAliases가 contact_resolution + contacts 2 SELECT를 돌려 N+1을 만들었음.
	aliasCache := buildSlackAliasCache(traceCtx, threads)

	for _, t := range threads {
		if err := slackLimiter.Wait(traceCtx); err != nil {
			return
		}
		processSingleSlackThread(traceCtx, sc, t, botID, aliasCache, wg)
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
	replies, _, _, err := sc.GetAPI().GetConversationReplies(params)
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

func handleThreadTimeout(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta) {
	msg := "Due to inactivity, this issue has been marked as resolved and monitoring is closed."
	_, _, _ = sc.GetAPI().PostMessage(t.ChannelID, slack.MsgOptionText(msg, false), slack.MsgOptionTS(t.ThreadTS))
	_ = store.CloseTargetedThread(ctx, t.ChannelID, t.ThreadTS, t.UserEmail)
}

func collectThreadCandidates(ctx context.Context, sc *channels.SlackClient, user *store.User, t store.SlackThreadMeta, replies []slack.Message, res threadScanResult, effAl []string) []types.RawMessage {
	var candidates []types.RawMessage

	for _, m := range replies {
		if t.LastTS != "" && m.Timestamp <= t.LastTS {
			continue
		}
		if res.isResolved && m.Timestamp > res.newLastTS {
			continue
		}
		isFromMe := strings.EqualFold(m.User, user.SlackID) || sc.GetUserName(m.User) == user.Name
		if isFromMe && completionSvc != nil && m.ThreadTimestamp != "" {
			completionSvc.ProcessPotentialCompletion(ctx, store.ConsolidatedMessage{
				UserEmail: user.Email, Source: "slack", ThreadID: t.ThreadTS, OriginalText: m.Text, SourceTS: m.Timestamp,
			})
		}
		// Why: Architecture Separation—Redundant isBot/Empty text checks are removed here as the channels layer already pre-filters these.
		c := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: t.ChannelID}}}
		cls := classifyMessage(c, user, effAl, types.RawMessage{Sender: m.User, Text: m.Text})
		if cls == types.CategoryTask || cls == types.CategoryQuery {
			candidates = append(candidates, types.RawMessage{
				ID: m.Timestamp, Sender: sc.GetUserName(m.User), Text: m.Text, Timestamp: channels.ParseSlackTimestamp(m.Timestamp),
				ReplyToID: t.ThreadTS, ChannelID: t.ChannelID, HasAttachment: len(m.Files) > 0,
				AttachmentNames: sc.ExtractFileNames(m.Files), Reactions: sc.ExtractReactions(m.Reactions), IsPinned: len(m.PinnedTo) > 0,
			})
		}
	}
	return candidates
}

func updateThreadStatus(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta, res threadScanResult) {
	if res.isResolved {
		msg := "This issue has been marked as resolved and monitoring is closed."
		_, _, _ = sc.GetAPI().PostMessage(t.ChannelID, slack.MsgOptionText(msg, false), slack.MsgOptionTS(t.ThreadTS))
		_ = store.CloseTargetedThread(ctx, t.ChannelID, t.ThreadTS, t.UserEmail)
		return
	}
	if res.newLastTS != t.LastTS || res.newLastActivity != t.LastActivityTS {
		_ = store.UpdateTargetedThread(ctx, t.ChannelID, t.ThreadTS, res.newLastTS, res.newLastActivity, t.UserEmail)
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
		for _, r := range m.Reactions {
			if r.Name == "white_check_mark" {
				isResolved = true
				break
			}
		}
		isBot := m.User == botID || m.BotID != ""
		if !isBot && !isResolved && m.Timestamp > newLastActivity {
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
	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to init Gemini client: %v", err)
		return
	}

	channelName := sc.GetChannelName(candidates[0].ChannelID)
	lockKey := roomLockSvc.GetRoomKey(user.Email, "slack", channelName)
	lock := roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	payload, msgMap := buildSlackAnalysisPayload(candidates, sc)
	lastMsg := candidates[len(candidates)-1]
	senderName := lastMsg.SenderName
	if senderName == "" {
		senderName = lastMsg.Sender
	}
	enriched, _ := EnrichSlackMessage(lastMsg.Sender, senderName, lastMsg.ChannelID, lastMsg.ReplyToID, payload, lastMsg.Timestamp)

	proposals, err := gc.Analyze(ctx, user.Email, *enriched, "Korean", "slack", channelName)
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Gemini Analyze Error for %s: %v", user.Email, err)
		return
	}

	// Why: [Service-Oriented Resolve] Ensures SLACK proposals are resolved using the same backend-driven similarity engine.
	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, "slack", channelName)
	items := tasksSvc.ResolveProposals(ctx, user.Email, channelName, proposals, tasks)
	processSlackItems(ctx, user, items, msgMap, sc, wg)
}

func processSlackItems(ctx context.Context, user *store.User, items []store.TodoItem, msgMap map[string]types.RawMessage, sc *channels.SlackClient, wg *sync.WaitGroup) {
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	var newIDs []int
	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			continue
		}
		msg := mapSlackItemToMessage(ctx, item, m, user, aliases, sc)

		// Routing Logic: Identifies resolve/update status before insertion.
		if id, _ := store.RouteTaskByStatus(ctx, nil, user.Email, item, msg); id > 0 {
			newIDs = append(newIDs, id)
			continue
		}

		id, err := store.HandleTaskState(ctx, nil, user.Email, item, msg)
		if err == nil && id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
}

func buildSlackAnalysisPayload(candidates []types.RawMessage, sc *channels.SlackClient) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range candidates {
		msgMap[m.ID] = m
		resolvedText := resolveSlackMentions(m.Text, sc)
		metaStr := buildSlackMetadataString(m)
		senderLabel := m.SenderName
		if senderLabel == "" {
			senderLabel = m.Sender
		}
		sb.WriteString(fmt.Sprintf("[ID:%s]%s %s: %s\n", m.ID, metaStr, senderLabel, resolvedText))
	}
	return sb.String(), msgMap
}

func buildSlackMetadataString(m types.RawMessage) string {
	var tags []string
	if m.IsPinned { tags = append(tags, "Pinned") }
	if m.IsImportant { tags = append(tags, "Important") }
	if m.IsForwarded { tags = append(tags, "Forwarded") }
	var sb strings.Builder
	if len(tags) > 0 { sb.WriteString(fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", "))) }
	if len(m.Reactions) > 0 { sb.WriteString(fmt.Sprintf(" [Reactions: %s]", strings.Join(m.Reactions, ", "))) }
	if len(m.AttachmentNames) > 0 { sb.WriteString(fmt.Sprintf(" [Files: %s]", strings.Join(m.AttachmentNames, ", "))) }
	return sb.String()
}

func mapSlackItemToMessage(ctx context.Context, item store.TodoItem, m types.RawMessage, user *store.User, aliases []string, sc *channels.SlackClient) store.ConsolidatedMessage {
	threadID := m.ReplyToID
	if threadID == "" {
		threadID = m.ID
	}
	link := buildSlackLinkAndRegisterThread(ctx, m, user.Email)

	params := services.TaskBuildParams{
		UserEmail:      user.Email,
		User:           *user,
		Aliases:        aliases,
		Item:           item,
		SenderRaw:      m.Sender, // Resolved Slack display name — primary identity fallback
		Source:         "slack",
		Room:           sc.GetChannelName(m.ChannelID),
		Link:           link,
		SourceTS:       m.ID,
		OriginalText:   m.Text,
		ThreadID:       threadID,
		SourceChannels: []string{"slack"},
	}
	return services.BuildTask(params)
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
		logger.Debugf("[INTAKE-SLACK] Thread registered for tracking: %s (User: %s)", threadTS, email)
	}
	return link
}
