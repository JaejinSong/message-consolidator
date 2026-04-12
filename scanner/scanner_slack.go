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

const SlackThrottlingInterval = 1200 * time.Millisecond

var slackLimiter = rate.NewLimiter(rate.Every(SlackThrottlingInterval), 1)

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

func scanSlack(ctx context.Context, users []store.User) {
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
	processSlackCandidates(ctx, users, sc, candidates)
	updateSlackCursors(newTS)
}

func prepareSlackUserAliases(ctx context.Context, users []store.User) map[string][]string {
	ua := make(map[string][]string)
	for _, u := range users {
		aliases, _ := store.GetUserAliases(ctx, u.ID)
		ua[u.Email] = getEffectiveAliases(u, aliases)
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
	if err := slackLimiter.Wait(ctx); err != nil {
		return err
	}
	msgs, err := sc.GetMessages(c.ID, time.Now().Add(-24*time.Hour), minTS)
	if err != nil || len(msgs) == 0 {
		return err
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
			completionSvc.ProcessPotentialCompletion(ctx, store.ConsolidatedMessage{
				UserEmail: u.Email, Source: "slack", ThreadID: m.ReplyToID, OriginalText: m.Text, SourceTS: m.ID,
			})
		}
		cls := classifyMessage(c, &u, userAl[u.Email], m)
		if cls == "내 업무" || cls == "회신 대기" {
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

func processSlackCandidates(ctx context.Context, users []store.User, sc *channels.SlackClient, candidates map[string][]types.RawMessage) {
	for email, msgs := range candidates {
		user, err := store.GetOrCreateUser(ctx, email, "", "")
		if err != nil || user == nil {
			continue
		}
		analyzeAndSaveSlack(ctx, user, sc, msgs)
	}
}

func updateSlackCursors(newTS map[string]map[string]string) {
	for email, channelMap := range newTS {
		for chanID, ts := range channelMap {
			store.UpdateLastScan(email, "slack", chanID, ts)
		}
	}
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m types.RawMessage) string {
	isFromMe := strings.EqualFold(m.Sender, user.Name) || strings.EqualFold(m.Sender, user.Email)
	if isFromMe && strings.Contains(m.Text, "<@U") && (user.SlackID == "" || !strings.Contains(m.Text, "<@"+user.SlackID+">")) {
		return "회신 대기"
	}
	if channel.IsIM || channel.IsMpIM {
		return "내 업무"
	}
	if user.SlackID != "" && strings.Contains(m.Text, "<@"+user.SlackID+">") {
		return "내 업무"
	}
	for _, alias := range aliases {
		if alias != "" && IsAliasMatched(m.Text, m.Sender, alias) {
			return "내 업무"
		}
	}
	return "기타 업무"
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
			sweepSlackThreads(ctx)
		}
	}
}

func sweepSlackThreads(ctx context.Context) {
	traceCtx, _ := trace.StartWithContext(ctx, "Background-SweepSlackThreads")
	defer trace.End(traceCtx, nil)

	if cfg == nil || cfg.SlackToken == "" {
		return
	}

	threads, err := store.GetTargetedActiveThreads(traceCtx)
	if err != nil || len(threads) == 0 {
		return
	}

	sc := channels.NewSlackClient(cfg.SlackToken)
	_ = sc.FetchUsers()

	auth, _ := sc.GetAPI().AuthTest()
	botID := ""
	if auth != nil {
		botID = auth.UserID
	}

	for _, t := range threads {
		if err := slackLimiter.Wait(traceCtx); err != nil {
			return
		}
		processSingleSlackThread(traceCtx, sc, t, botID)
	}
}

func processSingleSlackThread(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta, botID string) {
	if isThreadTimedOut(t.LastActivityTS, 7*24*time.Hour) {
		handleThreadTimeout(ctx, sc, t)
		return
	}

	user, _ := store.GetOrCreateUser(ctx, t.UserEmail, "", "")
	if user == nil {
		return
	}

	params := &slack.GetConversationRepliesParameters{
		ChannelID: t.ChannelID, Timestamp: t.ThreadTS, Oldest: t.LastTS, Limit: 100,
	}
	replies, _, _, err := sc.GetAPI().GetConversationReplies(params)
	if err != nil {
		return
	}

	res := scanThreadReplies(replies, t.LastTS, t.LastActivityTS, botID)
	candidates := collectThreadCandidates(ctx, sc, user, t, replies, res, botID)

	if len(candidates) > 0 {
		analyzeAndSaveSlack(ctx, user, sc, candidates)
	}
	updateThreadStatus(ctx, sc, t, res)
}

func handleThreadTimeout(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta) {
	msg := "Due to inactivity, this issue has been marked as resolved and monitoring is closed."
	_, _, _ = sc.GetAPI().PostMessage(t.ChannelID, slack.MsgOptionText(msg, false), slack.MsgOptionTS(t.ThreadTS))
	_ = store.CloseTargetedThread(ctx, t.ChannelID, t.ThreadTS, t.UserEmail)
}

func collectThreadCandidates(ctx context.Context, sc *channels.SlackClient, user *store.User, t store.SlackThreadMeta, replies []slack.Message, res threadScanResult, botID string) []types.RawMessage {
	var candidates []types.RawMessage
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	effAl := getEffectiveAliases(*user, aliases)

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
		isBot := m.User == botID || m.BotID != ""
		if !isBot && m.Text != "" {
			c := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: t.ChannelID}}}
			cls := classifyMessage(c, user, effAl, types.RawMessage{Sender: m.User, Text: m.Text})
			if cls == "내 업무" || cls == "회신 대기" {
				candidates = append(candidates, types.RawMessage{
					ID: m.Timestamp, Sender: sc.GetUserName(m.User), Text: m.Text, Timestamp: parseSlackTimestamp(m.Timestamp),
					ReplyToID: t.ThreadTS, ChannelID: t.ChannelID, HasAttachment: len(m.Files) > 0,
					AttachmentNames: sc.ExtractFileNames(m.Files), Reactions: sc.ExtractReactions(m.Reactions), IsPinned: len(m.PinnedTo) > 0,
				})
			}
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

func parseSlackTimestamp(ts string) time.Time {
	parts := strings.Split(ts, ".")
	if len(parts) == 0 {
		return time.Now()
	}
	sec, _ := strconv.ParseInt(parts[0], 10, 64)
	return time.Unix(sec, 0)
}

func analyzeAndSaveSlack(ctx context.Context, user *store.User, sc *channels.SlackClient, candidates []types.RawMessage) {
	if len(candidates) == 0 {
		return
	}
	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to init Gemini client: %v", err)
		return
	}

	channelName := sc.GetChannelName(candidates[0].ChannelID)
	payload, msgMap := buildSlackAnalysisPayload(candidates, sc)
	lastMsg := candidates[len(candidates)-1]
	enriched, _ := EnrichSlackMessage(lastMsg.Sender, sc.GetUserName(lastMsg.Sender), lastMsg.ChannelID, lastMsg.ReplyToID, payload, lastMsg.Timestamp)

	items, err := gc.Analyze(ctx, user.Email, *enriched, "Korean", "slack", channelName)
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Gemini Analyze Error for %s: %v", user.Email, err)
		return
	}
	processSlackItems(ctx, user, items, msgMap, sc)
}

func processSlackItems(ctx context.Context, user *store.User, items []store.TodoItem, msgMap map[string]types.RawMessage, sc *channels.SlackClient) {
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	var newIDs []int
	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			continue
		}
		msg := mapSlackItemToMessage(ctx, item, m, user, aliases, sc)
		id, err := store.HandleTaskState(ctx, nil, user.Email, item, msg)
		if err == nil && id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(user.Email, newIDs)
}

func buildSlackAnalysisPayload(candidates []types.RawMessage, sc *channels.SlackClient) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range candidates {
		msgMap[m.ID] = m
		resolvedText := resolveSlackMentions(m.Text, sc)
		metaStr := buildSlackMetadataString(m)
		sb.WriteString(fmt.Sprintf("[ID:%s]%s %s: %s\n", m.ID, metaStr, m.Sender, resolvedText))
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
	mChannel := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: m.ChannelID}}}
	classification := classifyMessage(mChannel, user, aliases, m)
	threadID := m.ReplyToID
	if threadID == "" { threadID = m.ID }
	link := buildSlackLinkAndRegisterThread(ctx, m, user.Email)
	category := item.Category
	if classification == "회신 대기" { category = "waiting" }
	return store.ConsolidatedMessage{
		UserEmail: user.Email, Source: "slack", Room: sc.GetChannelName(m.ChannelID),
		Task: item.Task, Requester: item.Requester, Assignee: normalizeSlackAssignee(item.Assignee, user),
		AssignedAt: m.Timestamp, Link: link, SourceTS: m.ID, OriginalText: m.Text, Category: category,
		ThreadID: threadID, SourceChannels: []string{"slack"},
	}
}

func normalizeSlackAssignee(assignee string, user *store.User) string {
	lowerAsg := strings.ToLower(strings.TrimSpace(assignee))
	if lowerAsg == "" { return services.AssigneeShared }
	if lowerAsg == "me" || lowerAsg == "__current_user__" || lowerAsg == "나" || strings.EqualFold(assignee, user.Name) {
		if user.Name != "" { return user.Name }
		return user.Email
	}
	return assignee
}

func buildSlackLinkAndRegisterThread(ctx context.Context, m types.RawMessage, email string) string {
	link := fmt.Sprintf("https://slack.com/archives/%s/p%s", m.ChannelID, strings.ReplaceAll(m.ID, ".", ""))
	if m.ReplyToID != "" {
		if err := store.RegisterTargetedSlackThread(ctx, m.ChannelID, m.ReplyToID, m.ID, email); err == nil {
			logger.Debugf("[INTAKE-SLACK] Targeted thread registered: %s (User: %s)", m.ReplyToID, email)
		}
		link += fmt.Sprintf("?thread_ts=%s", m.ReplyToID)
	}
	return link
}
