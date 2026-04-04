package scanner

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sync"

	"github.com/slack-go/slack"
	"github.com/whatap/go-api/trace"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

//Why: Pre-compiling the regex avoids repeated compilation overhead during high-volume message processing.
var slackMentionRegex = regexp.MustCompile(`<@([A-Z0-9]+)>`)

//Why: Enforces a 1.2s delay between API calls to strictly comply with Slack's Tier 3 rate limits and prevent HTTP 429 errors.
const SlackThrottlingInterval = 1200 * time.Millisecond

//Why: A global rate limiter ensures that concurrent user scans do not collectively breach Slack's workspace-level API limits.
var slackLimiter = rate.NewLimiter(rate.Every(SlackThrottlingInterval), 1)

//Why: Abstracts the Slack client dependency to allow deterministic unit testing without requiring actual API connections.
type slackUserResolver interface {
	GetUserName(userID string) string
}

//Why: Gemini struggles with raw Slack user IDs (e.g., <@U123>). Resolving them to human-readable names improves AI extraction accuracy.
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

//Why: Scanning channels per user leads to redundant API calls and rate limit exhaustion. We scan each channel exactly once and map tasks to users in memory (Batching).
func scanSlack(ctx context.Context, users []store.User) {
	if cfg == nil || cfg.SlackToken == "" || len(users) == 0 {
		return
	}
	logger.Debugf("[SCAN-SLACK] Starting global Slack scan for %d users", len(users))

	sc := channels.NewSlackClient(cfg.SlackToken)
	if err := sc.FetchUsers(); err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch users: %v", err)
	}

	//Why: Retrieves only the public and private channels the bot has been explicitly invited to, minimizing unnecessary API overhead.
	chans, _, err := sc.LookupChannels()
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch channels: %v", err)
		return
	}

	//Why: Pre-compute user aliases to avoid redundant DB queries during the inner message scanning loop.
	userAliases := make(map[string][]string)
	for _, u := range users {
		aliases, _ := store.GetUserAliases(u.ID)
		userAliases[u.Email] = getEffectiveAliases(u, aliases)
	}

	//Why: Accumulates potential tasks from all scanned channels into a per-user memory buffer to enable bulk AI analysis, which reduces API latency and token costs.
	globalCandidates := make(map[string][]types.RawMessage)
	//Why: Tracks the latest message timestamp processed per user-channel pair to update the scan cursors correctly after a successful batch run.
	globalNewTS := make(map[string]map[string]string) // UserEmail -> ChannelID -> NewTS
	var candidateMu sync.Mutex

	var eg errgroup.Group
	eg.SetLimit(3)

	for _, channel := range chans {
		c := channel
		eg.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}

			//Why: To support batching, we must fetch messages starting from the oldest lastTS among all users in this channel.
			minLastTS := ""
			for _, u := range users {
				ts := store.GetLastScan(u.Email, "slack", c.ID)
				if ts == "" {
					minLastTS = ""
					break
				}
				if minLastTS == "" || ts < minLastTS {
					minLastTS = ts
				}
			}

			//Why: Ensure we don't violate global Slack API rate limits before fetching history.
			if err := slackLimiter.Wait(ctx); err != nil {
				return err
			}

			msgs, err := sc.GetMessages(c.ID, time.Now().Add(-24*time.Hour), minLastTS)
			if err != nil {
				logger.Warnf("[SCAN-SLACK] Failed to fetch messages for channel %s: %v", c.Name, err)
				return err
			}

			if len(msgs) == 0 {
				return nil
			}

			candidateMu.Lock()
			defer candidateMu.Unlock()

			for _, m := range msgs {
				for _, u := range users {
					userLastTS := store.GetLastScan(u.Email, "slack", c.ID)
					if userLastTS != "" && m.ID <= userLastTS {
						continue
					}

					isFromMe := strings.EqualFold(m.Sender, u.Name) || strings.EqualFold(m.Sender, u.Email)
					if isFromMe && completionSvc != nil && m.ReplyToID != "" {
						completionSvc.ProcessPotentialCompletion(ctx, store.ConsolidatedMessage{
							UserEmail:    u.Email,
							Source:       "slack",
							ThreadID:     m.ReplyToID,
							OriginalText: m.Text,
							SourceTS:     m.ID,
						})
					}

					classification := classifyMessage(c, &u, userAliases[u.Email], m)
					if classification == "내 업무" || classification == "회신 대기" {
						m.ChannelID = c.ID
						globalCandidates[u.Email] = append(globalCandidates[u.Email], m)
					}

					if globalNewTS[u.Email] == nil {
						globalNewTS[u.Email] = make(map[string]string)
					}
					if currTS, exists := globalNewTS[u.Email][c.ID]; !exists || m.ID > currTS {
						globalNewTS[u.Email][c.ID] = m.ID
					}
				}
			}
			return nil
		})
	}
	_ = eg.Wait()

	//Why: Triggers the final AI extraction phase for all candidates collected during the channel-wide scan, ensuring high context awareness for related messages.
	consecutiveFailures := 0
	for email, candidates := range globalCandidates {
		//Why: A circuit breaker prevents infinite error loops and database overload if the backend is down.
		if consecutiveFailures >= 5 {
			logger.Errorf("[SCAN-SLACK] Circuit breaker triggered after 5 consecutive user store failures. Aborting batch analysis.")
			break
		}

		user, err := store.GetOrCreateUser(email, "", "")
		if err != nil || user == nil {
			logger.Errorf("[SCAN-SLACK] Failed to get user %s: %v. Skipping.", email, err)
			consecutiveFailures++
			continue
		}
		consecutiveFailures = 0 //Why: Resets the circuit breaker counter upon a successful user record retrieval to allow subsequent scans to proceed.

		analyzeAndSaveSlack(ctx, user, sc, candidates)
	}

	//Why: Permanently stores the new scan cursor in the database for each user and channel to ensure the next interval starts from where it left off.
	for email, channelMap := range globalNewTS {
		for chanID, newTS := range channelMap {
			store.UpdateLastScan(email, "slack", chanID, newTS)
		}
	}
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m types.RawMessage) string {
	isFromMe := strings.EqualFold(m.Sender, user.Name) || strings.EqualFold(m.Sender, user.Email)

	//Why: If the user sends a message mentioning someone else, it usually implies they are waiting for a reply.
	if isFromMe && strings.Contains(m.Text, "<@U") && (user.SlackID == "" || !strings.Contains(m.Text, "<@"+user.SlackID+">")) {
		return "회신 대기"
	}

	//Why: Direct messages or explicit mentions are high-signal indicators of tasks belonging to the user.
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
			logger.Infof("Slack Slow Sweeper stopping...")
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

	//Why: Retrieves metadata for Slack threads that are currently marked as active and awaiting resolution, allowing the sweeper to focus on high-priority items.
	threads, err := store.GetTargetedActiveThreads()
	if err != nil || len(threads) == 0 {
		return
	}

	sc := channels.NewSlackClient(cfg.SlackToken)
	if err := sc.FetchUsers(); err != nil {
		logger.Warnf("[SCAN-SWEEPER] Failed to fetch users: %v", err)
	}

	//Why: Determines the bot's own internal Slack ID to filter out its own notification messages and prevent accidental recursive sweep cycles.
	auth, err := sc.GetAPI().AuthTest()
	botID := ""
	if err == nil {
		botID = auth.UserID
	}

	for _, t := range threads {
		//Why: Respects the global workspace-wide Slack rate limit before processing each individual thread metadata, ensuring stable long-term operation.
		if err := slackLimiter.Wait(traceCtx); err != nil {
			return
		}

		processSingleSlackThread(traceCtx, sc, t, botID)
	}
}

func processSingleSlackThread(ctx context.Context, sc *channels.SlackClient, t store.SlackThreadMeta, botID string) {
	//Why: Auto-close threads after 7 days of inactivity to prevent the database of active threads from growing indefinitely.
	if isThreadTimedOut(t.LastActivityTS, 7*24*time.Hour) {
		closeMsg := "Due to inactivity, this issue has been marked as resolved and monitoring is closed. Please create a new thread for further technical support."
		_, _, _ = sc.GetAPI().PostMessage(t.ChannelID, slack.MsgOptionText(closeMsg, false), slack.MsgOptionTS(t.ThreadTS))
		_ = store.CloseTargetedThread(t.ChannelID, t.ThreadTS, t.UserEmail)
		return
	}

	user, err := store.GetOrCreateUser(t.UserEmail, "", "")
	if err != nil || user == nil {
		logger.Errorf("[SCAN-SWEEPER] Failed to get user %s for thread %s", t.UserEmail, t.ThreadTS)
		return
	}
	aliases, _ := store.GetUserAliases(user.ID)
	effectiveAliases := getEffectiveAliases(*user, aliases)

	//Why: Retrieves only the newest replies in a target thread since the previous sweep cycle to minimize message extraction redundancy.
	replies, _, _, err := sc.GetAPI().GetConversationReplies(&slack.GetConversationRepliesParameters{
		ChannelID: t.ChannelID,
		Timestamp: t.ThreadTS,
		Oldest:    t.LastTS,
		Limit:     100,
	})
	if err != nil || len(replies) == 0 {
		return
	}

	result := scanThreadReplies(replies, t.LastTS, t.LastActivityTS, botID)

	//Why: We must collect candidates before checking the resolved status to ensure the final message that triggered the resolution is also analyzed.
	var candidates []types.RawMessage
	for _, m := range replies {
		//Why: Skip messages already processed in previous sweeps to avoid duplicate task extraction.
		if t.LastTS != "" && m.Timestamp <= t.LastTS {
			continue
		}

		//Why: Sync candidates with the pure-logic scan result to strictly ignore messages arriving after the thread was resolved.
		if result.isResolved && m.Timestamp > result.newLastTS {
			continue
		}

		isFromMe := strings.EqualFold(m.User, user.SlackID) || sc.GetUserName(m.User) == user.Name
		if isFromMe && completionSvc != nil && m.ThreadTimestamp != "" {
			completionSvc.ProcessPotentialCompletion(ctx, store.ConsolidatedMessage{
				UserEmail:    user.Email,
				Source:       "slack",
				ThreadID:     t.ThreadTS,
				OriginalText: m.Text,
				SourceTS:     m.Timestamp,
			})
		}

		isBot := m.User == botID || m.BotID != ""
		if !isBot && m.Text != "" {
			classification := classifyMessage(slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: t.ChannelID}}}, user, effectiveAliases, types.RawMessage{Sender: m.User, Text: m.Text})
			if classification == "내 업무" || classification == "회신 대기" {
				candidates = append(candidates, types.RawMessage{
					ID:        m.Timestamp,
					Sender:    sc.GetUserName(m.User),
					Text:      m.Text,
					Timestamp: parseSlackTimestamp(m.Timestamp),
					ReplyToID: t.ThreadTS,
					ChannelID: t.ChannelID,
				})
			}
		}
	}

	if len(candidates) > 0 {
		analyzeAndSaveSlack(ctx, user, sc, candidates)
	}

	if result.isResolved {
		closeMsg := "This issue has been marked as resolved and monitoring is closed. Please create a new thread for further technical support."
		_, _, _ = sc.GetAPI().PostMessage(t.ChannelID, slack.MsgOptionText(closeMsg, false), slack.MsgOptionTS(t.ThreadTS))
		_ = store.CloseTargetedThread(t.ChannelID, t.ThreadTS, t.UserEmail)
		return
	}

	if result.newLastTS != t.LastTS || result.newLastActivity != t.LastActivityTS {
		_ = store.UpdateTargetedThread(t.ChannelID, t.ThreadTS, result.newLastTS, result.newLastActivity, t.UserEmail)
	}
}

//Why: Holds the pure-logic output of scanning a thread's replies to separate state calculation from side-effect execution.
type threadScanResult struct {
	isResolved      bool
	newLastTS       string
	newLastActivity string
}

//Why: Extracted as a pure function without side effects (API/DB calls) to make the core thread logic easily unit-testable.
func scanThreadReplies(replies []slack.Message, lastTS, lastActivityTS, botID string) threadScanResult {
	newLastTS := lastTS
	newLastActivity := lastActivityTS
	isResolved := false

	for _, m := range replies {
		//Why: Ensure we only process new replies since the last sweep, regardless of whether it's the parent message or a child, to prevent duplicate analysis.
		if lastTS != "" && m.Timestamp <= lastTS {
			continue
		}

		//Why: A 'white_check_mark' reaction is the agreed-upon convention for users to mark a thread as resolved.
		for _, r := range m.Reactions {
			if r.Name == "white_check_mark" {
				isResolved = true
				break
			}
		}

		//Why: Bot messages and resolution messages should not extend the thread's lifespan.
		isBot := m.User == botID || m.BotID != ""
		if !isBot && !isResolved && m.Timestamp > newLastActivity {
			newLastActivity = m.Timestamp
		}

		if m.Timestamp > newLastTS {
			newLastTS = m.Timestamp
		}

		//Why: Stop scanning immediately upon resolution to prevent post-resolution chatter from reopening the thread.
		// newLastTS is updated before this check so the resolution message itself is marked as scanned.
		if isResolved {
			break
		}
	}

	return threadScanResult{
		isResolved:      isResolved,
		newLastTS:       newLastTS,
		newLastActivity: newLastActivity,
	}
}

// Why: Helper function to determine if a thread has exceeded its allowed inactivity window.
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

	// Why: [Context Enrichment] Packages the cumulative payload with metadata from the latest candidate for AI analysis.
	lastMsg := candidates[len(candidates)-1]
	enriched, _ := EnrichSlackMessage(lastMsg.Sender, sc.GetUserName(lastMsg.Sender), lastMsg.ChannelID, lastMsg.ReplyToID, payload, lastMsg.Timestamp)

	items, err := gc.Analyze(ctx, user.Email, *enriched, "Korean", "slack", channelName)
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Gemini Analyze Error for %s: %v", user.Email, err)
		return
	}
	processSlackItems(user, items, msgMap, sc)
}

func processSlackItems(user *store.User, items []store.TodoItem, msgMap map[string]types.RawMessage, sc *channels.SlackClient) {
	aliases, _ := store.GetUserAliases(user.ID)
	var newIDs []int
	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok { continue }

		msg := mapSlackItemToMessage(item, m, user, aliases, sc)
		id, err := store.HandleTaskState(user.Email, item, msg)
		if err == nil && id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(user.Email, newIDs)
}

//Why: Separating payload construction keeps the main workflow function clean and focused.
func buildSlackAnalysisPayload(candidates []types.RawMessage, sc *channels.SlackClient) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range candidates {
		msgMap[m.ID] = m
		resolvedText := resolveSlackMentions(m.Text, sc)
		sb.WriteString(fmt.Sprintf("[ID:%s] %s: %s\n", m.ID, m.Sender, resolvedText))
	}
	return sb.String(), msgMap
}

//Why: Encapsulating the complex mapping and side-effects (thread registration) improves readability.
func mapSlackItemToMessage(item store.TodoItem, m types.RawMessage, user *store.User, aliases []string, sc *channels.SlackClient) store.ConsolidatedMessage {
	mChannel := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: m.ChannelID}}}
	classification := classifyMessage(mChannel, user, aliases, m)

	threadID := m.ReplyToID
	if threadID == "" {
		threadID = m.ID
	}

	link := buildSlackLinkAndRegisterThread(m, user.Email)
	assignee := normalizeSlackAssignee(item.Assignee, user)
	category := item.Category

	if classification == "회신 대기" {
		category = "waiting"
	}

	return store.ConsolidatedMessage{
		UserEmail:    user.Email,
		Source:       "slack",
		Room:         sc.GetChannelName(m.ChannelID),
		Task:         item.Task,
		Requester:    item.Requester,
		Assignee:     assignee,
		AssignedAt:   m.Timestamp,
		Link:         link,
		SourceTS:     m.ID,
		OriginalText: m.Text,
		Category:     category,
		ThreadID:     threadID,
		SourceChannels: []string{"slack"}, // Initial source for the new task
	}
}

//Why: Isolates the specific heuristic logic for deciding when an assignee should be defaulted to the current user.
func normalizeSlackAssignee(assignee string, user *store.User) string {
	lowerAsg := strings.ToLower(assignee)
	if lowerAsg == "" || lowerAsg == "me" || lowerAsg == "나" || strings.EqualFold(assignee, user.Name) {
		if user.Name != "" {
			return user.Name
		}
		return user.Email
	}
	return assignee
}

//Why: Abstracts the URL formatting and the side-effect of registering threads for continuous background sweeping.
func buildSlackLinkAndRegisterThread(m types.RawMessage, email string) string {
	link := fmt.Sprintf("https://slack.com/archives/%s/p%s", m.ChannelID, strings.ReplaceAll(m.ID, ".", ""))
	//Why: Register the thread so the slow sweeper can continuously monitor it for follow-up replies.
	if m.ReplyToID != "" {
		if err := store.RegisterTargetedSlackThread(m.ChannelID, m.ReplyToID, m.ID, email); err == nil {
			logger.Debugf("[INTAKE-SLACK] Targeted thread registered: %s (User: %s)", m.ReplyToID, email)
		}
		link += fmt.Sprintf("?thread_ts=%s", m.ReplyToID)
	}
	return link
}
