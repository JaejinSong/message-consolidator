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

	"github.com/slack-go/slack"
	"github.com/whatap/go-api/trace"
	"golang.org/x/sync/errgroup"
)

// slackMentionRegex는 Slack 멘션 포맷(<@U12345678>)을 찾기 위한 정규식입니다.
var slackMentionRegex = regexp.MustCompile(`<@([A-Z0-9]+)>`)

// slackUserResolver는 테스트 용이성을 위해 SlackClient의 의존성을 분리하는 인터페이스입니다.
type slackUserResolver interface {
	GetUserName(userID string) string
}

// resolveSlackMentions는 Slack 멘션(<@U...>)을 실제 사용자 이름(@Jaejin Song)으로 치환합니다.
// API 호출을 최소화하기 위해 Slack 클라이언트 내의 사용자 캐시를 활용합니다.
func resolveSlackMentions(text string, sc slackUserResolver) string {
	return slackMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		// ID 추출: <@U0208BU06JE> -> U0208BU06JE
		userID := match[2 : len(match)-1]

		// SlackClient에 캐시된 사용자 정보 조회 (sc.FetchUsers()가 미리 호출되어 있어야 함)
		userName := sc.GetUserName(userID)

		// 사용자 이름을 찾으면 @이름 형식으로, 못찾으면 원본 멘션 형식으로 반환
		if userName != "" && userName != userID {
			return "@" + userName
		}
		return match
	})
}

// scanSlack은 봇이 참여한 모든 채널을 한 번만 스캔하고, 가져온 메시지를 전체 유저에 대해 평가합니다.
func scanSlack(ctx context.Context, users []store.User) {
	if cfg.SlackToken == "" || len(users) == 0 {
		return
	}
	logger.Debugf("[SCAN-SLACK] Starting global Slack scan for %d users", len(users))

	sc := channels.NewSlackClient(cfg.SlackToken)
	if err := sc.FetchUsers(); err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch users: %v", err)
	}

	// Use the Slack API to get channels the bot is a member of
	chans, _, err := sc.LookupChannels()
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch channels: %v", err)
		return
	}

	// 유저별 별칭 맵 미리 구성
	userAliases := make(map[string][]string)
	for _, u := range users {
		aliases, _ := store.GetUserAliases(u.ID)
		userAliases[u.Email] = getEffectiveAliases(u, aliases)
	}

	var eg errgroup.Group
	// API Rate Limit 방어를 위해 한 번에 3개 채널씩만 천천히 스캔합니다.
	eg.SetLimit(3)

	for _, channel := range chans {
		c := channel
		eg.Go(func() error {
			// 시작 전이나 처리 도중 타임아웃이 발생했는지 감지
			if err := ctx.Err(); err != nil {
				return err
			}

			// 여러 유저 중 가장 오래된 lastTS 탐색 (API 호출 범위 최소화)
			minLastTS := ""
			for _, u := range users {
				ts := store.GetLastScan(u.Email, "slack", c.ID)
				if ts == "" {
					minLastTS = "" // 한번이라도 빈 값이면 채널 처음부터 스캔 (24시간 전 제한 작동)
					break
				}
				if minLastTS == "" || ts < minLastTS {
					minLastTS = ts
				}
			}

			msgs, err := sc.GetMessages(c.ID, time.Now().Add(-24*time.Hour), minLastTS)
			if err != nil {
				logger.Warnf("[SCAN-SLACK] Failed to fetch messages for channel %s: %v", c.Name, err)
				return err
			}

			if len(msgs) == 0 {
				return nil
			}

			// Pre-filtering: Collect candidate messages that definitely contain tasks for each user
			// map[UserEmail][]types.RawMessage
			userCandidates := make(map[string][]types.RawMessage)
			newUserLastTS := make(map[string]string)

			for _, m := range msgs {
				for _, u := range users {
					userLastTS := store.GetLastScan(u.Email, "slack", c.ID)
					if userLastTS != "" && m.ID <= userLastTS {
						continue
					}

					classification := classifyMessage(c, &u, userAliases[u.Email], m)
					if classification == "내 업무" || classification == "회신 대기" {
						userCandidates[u.Email] = append(userCandidates[u.Email], m)
					}

					if currTS, exists := newUserLastTS[u.Email]; !exists || m.ID > currTS {
						newUserLastTS[u.Email] = m.ID
					}
				}
			}

			// AI Analysis: Process candidates for each user
			for email, candidates := range userCandidates {
				user, _ := store.GetOrCreateUser(email, "", "")
				analyzeAndSaveSlack(ctx, user, sc, c.ID, candidates)
			}

			for email, newTS := range newUserLastTS {
				store.UpdateLastScan(email, "slack", c.ID, newTS)
			}

			// API Burst 방어를 위한 미세한 대기 시간
			time.Sleep(500 * time.Millisecond)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		logger.Debugf("[SCAN-SLACK] Partially completed with errors: %v", err)
	}
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m types.RawMessage) string {
	// 1. 발신자가 나인지 확인
	isFromMe := strings.EqualFold(m.Sender, user.Name) || strings.EqualFold(m.Sender, user.Email)

	// 내가 보낸 메시지인데, 다른 사람을 멘션(@U...)했다면 회신 대기로 판단
	if isFromMe && strings.Contains(m.Text, "<@U") && (user.SlackID == "" || !strings.Contains(m.Text, "<@"+user.SlackID+">")) {
		return "회신 대기"
	}

	// 2. DM이거나 내가 멘션된 경우 내 업무 판단
	// 1. DM이거나 직접 멘션된 경우 즉시 반환 (Early Return)
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

func startSlowSweeper() {
	logger.Infof("Slack Slow Sweeper started (monitoring old threads)...")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sweepSlackThreads()
	}
}

func sweepSlackThreads() {
	traceCtx, _ := trace.StartWithContext(context.Background(), "Background-SweepSlackThreads")
	defer trace.End(traceCtx, nil)

	if cfg == nil || cfg.SlackToken == "" {
		return
	}

	threads, err := store.GetActiveSlackThreads()
	if err != nil || len(threads) == 0 {
		return
	}

	sc := channels.NewSlackClient(cfg.SlackToken)
	if err := sc.FetchUsers(); err != nil {
		logger.Warnf("[SCAN-SWEEPER] Failed to fetch users: %v", err)
	}

	for _, t := range threads {
		// 14일 이상 경과한 오래된 스레드는 모니터링 종료 (자동 정리)
		tsParts := strings.Split(t.ThreadTS, ".")
		if len(tsParts) > 0 {
			if sec, err := strconv.ParseInt(tsParts[0], 10, 64); err == nil {
				if time.Since(time.Unix(sec, 0)) > 14*24*time.Hour {
					_ = store.RemoveActiveSlackThread(t.UserEmail, t.ChannelID, t.ThreadTS)
					continue
				}
			}
		}

		user, err := store.GetOrCreateUser(t.UserEmail, "", "")
		if err != nil {
			continue
		}
		aliases, _ := store.GetUserAliases(user.ID)
		effectiveAliases := getEffectiveAliases(*user, aliases)

		replies, err := sc.FetchNewThreadReplies(t.ChannelID, t.ThreadTS, t.LastTS)
		if err != nil || len(replies) == 0 {
			time.Sleep(3 * time.Second) // Rate Limit 방어를 위한 지연
			continue
		}

		newLastTS := t.LastTS
		var candidates []types.RawMessage
		for _, m := range replies {
			classification := classifyMessage(slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: t.ChannelID}}}, user, effectiveAliases, m)
			if classification == "내 업무" || classification == "회신 대기" {
				candidates = append(candidates, m)
			}
			if m.ID > newLastTS {
				newLastTS = m.ID
			}
		}

		if len(candidates) > 0 {
			analyzeAndSaveSlack(traceCtx, user, sc, t.ChannelID, candidates)
		}

		if newLastTS != t.LastTS {
			_ = store.UpdateSlackThreadLastTS(t.UserEmail, t.ChannelID, t.ThreadTS, newLastTS)
		}
		time.Sleep(3 * time.Second) // API 호출 후 무조건 3초 대기 (분당 20회 제한 준수)
	}
}

// analyzeAndSaveSlack encapsulates Gemini analysis and persistence for Slack messages.
func analyzeAndSaveSlack(ctx context.Context, user *store.User, sc *channels.SlackClient, channelID string, candidates []types.RawMessage) {
	if len(candidates) == 0 {
		return
	}

	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to init Gemini client: %v", err)
		return
	}

	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range candidates {
		msgMap[m.ID] = m
		resolvedText := resolveSlackMentions(m.Text, sc)
		sb.WriteString(fmt.Sprintf("[ID:%s] %s: %s\n", m.ID, m.Sender, resolvedText))
	}

	items, err := gc.Analyze(ctx, user.Email, sb.String(), "Korean", "slack")
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Gemini Analyze Error for %s: %v", user.Email, err)
		return
	}

	var msgsToSave []store.ConsolidatedMessage
	aliases, _ := store.GetUserAliases(user.ID)

	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			continue
		}

		// Classification for saving metadata
		classification := classifyMessage(slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: channelID}}}, user, aliases, m)

		threadID := m.ReplyToID
		if threadID == "" {
			threadID = m.ID
		}

		link := fmt.Sprintf("https://slack.com/archives/%s/p%s", channelID, strings.ReplaceAll(m.ID, ".", ""))
		if m.ReplyToID != "" {
			_ = store.RegisterActiveSlackThread(user.Email, channelID, m.ReplyToID)
			link += fmt.Sprintf("?thread_ts=%s", m.ReplyToID)
		}

		assignee := item.Assignee
		category := item.Category

		// Handle "Me" assignment logic
		lowerAsg := strings.ToLower(assignee)
		if lowerAsg == "" || lowerAsg == "me" || lowerAsg == "나" || strings.EqualFold(assignee, user.Name) {
			assignee = user.Name
			if assignee == "" {
				assignee = user.Email
			}
		}

		if classification == "회신 대기" {
			category = "waiting"
		}

		msgsToSave = append(msgsToSave, store.ConsolidatedMessage{
			UserEmail:    user.Email,
			Source:       "slack",
			Room:         sc.GetChannelName(channelID),
			Task:         item.Task,
			Requester:    item.Requester,
			Assignee:     assignee,
			AssignedAt:   m.Timestamp,
			Link:         link,
			SourceTS:     m.ID,
			OriginalText: m.Text,
			Category:     category,
			ThreadID:     threadID,
		})
	}

	if len(msgsToSave) > 0 {
		store.SaveMessages(msgsToSave)
	}
}
