package main

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
)

func startBackgroundScanner() {
	logger.Infof("Background scanner started (59s interval for anti-resonance)...")
	ticker := time.NewTicker(59 * time.Second)
	defer ticker.Stop()

	// Run initial scan
	RunAllScans()

	for range ticker.C {
		RunAllScans()
	}
}

func RunAllScans() {
	users, err := store.GetAllUsers()
	if err != nil {
		logger.Errorf("Scanner Error: Failed to get users: %v", err)
		return
	}
	var wg sync.WaitGroup
	for _, user := range users {
		// Get aliases for this user
		aliases, _ := store.GetUserAliases(user.ID)

		wg.Add(1)
		go func(u store.User, al []string) {
			defer wg.Done()
			scanAllSources(u, al)
		}(user, aliases)
	}
	wg.Wait()

	// 글로벌 유지보수 작업 (사용자 루프 밖에서 1번만 실행)
	if err := store.ArchiveOldTasks(); err != nil {
		logger.Errorf("Scanner Error: Failed to archive old tasks: %v", err)
	}

	// 주기적(1시간)으로 메모리에 쌓인 미반영 토큰 사용량을 DB에 플러시하여 NeonDB Sleep 보장
	store.FlushTokenUsageIfNeeded()
}

// getEffectiveAliases는 사용자가 명시적으로 등록한 별칭 외에
// 본인의 이름과 이메일 아이디를 자동으로 포함시켜 스캔 시 감지 누락을 방지합니다.
func getEffectiveAliases(user store.User, aliases []string) []string {
	effective := append([]string{}, aliases...)
	if user.Name != "" {
		effective = append(effective, user.Name)
	}
	if user.Email != "" {
		if idx := strings.Index(user.Email, "@"); idx != -1 {
			effective = append(effective, user.Email[:idx])
		}
	}
	return effective
}

func scanAllSources(user store.User, aliases []string) {
	logger.Debugf("[SCAN] Scanning for user: %s", user.Email)

	var wg sync.WaitGroup
	effectiveAliases := getEffectiveAliases(user, aliases)

	// 1. Gmail 스캔 (병렬 처리)
	if store.HasGmailToken(user.Email) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Debugf("[SCAN] Starting Gmail scan for %s", user.Email)
			channels.ScanGmail(context.Background(), user.Email, "Korean", cfg)
		}()
	}

	// 2. Slack 스캔 (병렬 처리)
	if cfg.SlackToken != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Debugf("[SCAN] Starting Slack scan for %s", user.Email)
			scanSlack(user, effectiveAliases)
		}()
	}

	// 3. WhatsApp 스캔 (병렬 처리 및 누락된 호출 복구)
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Debugf("[SCAN] Starting WhatsApp scan for %s", user.Email)
		scanWhatsApp(context.Background(), user, effectiveAliases, "Korean")
	}()

	wg.Wait() // 모든 채널의 스캔이 끝날 때까지 대기

	// Persistence of scan metadata
	store.PersistAllScanMetadata(user.Email)
}

func scan(email string, lang string) {
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		logger.Errorf("[SCAN] Failed to get user %s: %v", email, err)
		return
	}
	aliases, _ := store.GetUserAliases(user.ID)
	effectiveAliases := getEffectiveAliases(*user, aliases)

	// Gmail
	if store.HasGmailToken(email) {
		channels.ScanGmail(context.Background(), email, lang, cfg)
	}

	// Slack
	scanSlack(*user, effectiveAliases)

	// WhatsApp
	scanWhatsApp(context.Background(), *user, effectiveAliases, lang)
}

func scanSlack(user store.User, aliases []string) {
	if cfg.SlackToken == "" {
		return
	}
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

	for _, channel := range chans {
		lastTS := store.GetLastScan(user.Email, "slack", channel.ID)
		msgs, err := sc.GetMessages(channel.ID, time.Now().Add(-24*time.Hour), lastTS)
		if err != nil {
			logger.Warnf("[SCAN-SLACK] Failed to fetch messages for channel %s (user: %s): %v", channel.Name, user.Email, err)
			continue
		}

		if len(msgs) == 0 {
			continue
		}

		var msgsToSave []store.ConsolidatedMessage
		newLastTS := lastTS
		for _, m := range msgs {
			classification := classifyMessage(channel, &user, aliases, m)
			if classification == "내 업무" {
				link := fmt.Sprintf("https://slack.com/archives/%s/p%s", channel.ID, strings.ReplaceAll(m.ID, ".", ""))

				assigneeName := user.Name
				if assigneeName == "" {
					assigneeName = user.Email
				}

				msgsToSave = append(msgsToSave, store.ConsolidatedMessage{
					UserEmail:    user.Email,
					Source:       "slack",
					Room:         sc.GetChannelName(channel.ID),
					Task:         m.Text,
					Requester:    m.Sender,
					Assignee:     assigneeName,
					AssignedAt:   m.Timestamp.Format(time.RFC3339),
					Link:         link,
					SourceTS:     m.ID,
					OriginalText: m.Text,
				})
			}
			if m.ID > newLastTS {
				newLastTS = m.ID
			}
		}
		if len(msgsToSave) > 0 {
			store.SaveMessages(msgsToSave)
		}
		store.UpdateLastScan(user.Email, "slack", channel.ID, newLastTS)
	}
}

// IsAliasMatched는 짧은 별칭('나', 'me')이나 일반 대명사로 인한 오탐을 방지하기 위한 안전한 매칭 함수입니다.
func IsAliasMatched(text, sender, alias string) bool {
	lowerAlias := strings.ToLower(strings.TrimSpace(alias))
	if lowerAlias == "" {
		return false
	}
	aliasLen := len([]rune(lowerAlias))

	// 1. 발신자 일치 검사
	if sender != "" {
		lowerSender := strings.ToLower(sender)
		if lowerSender == lowerAlias {
			return true
		}
		// 2글자 이상인 경우만 발신자 이름 부분 일치 허용 ('나'가 '유나'에 매칭되는 것 방지)
		if aliasLen > 1 && strings.Contains(lowerSender, lowerAlias) {
			return true
		}
	}

	// 2. 본문 일치 검사
	if text != "" {
		lowerText := strings.ToLower(text)
		if aliasLen <= 2 {
			// 짧은 별칭은 띄어쓰기 단위로 분리하여 엄격하게 검사
			words := strings.Fields(lowerText)
			for _, w := range words {
				// 완전히 일치하거나, 한국어 조사 처리를 위해 접두어로 쓰인 경우 허용 ("나", "나는", "me")
				if w == lowerAlias || (strings.HasPrefix(w, lowerAlias) && len([]rune(w)) <= aliasLen+2) {
					return true
				}
			}
		} else {
			// 길이가 긴 고유 별칭은 기존처럼 유연하게 부분 일치 허용
			if strings.Contains(lowerText, lowerAlias) {
				return true
			}
		}
	}

	return false
}

func resolveWAMentions(email, text string) string {
	// WhatsApp mentions are "@12345678"
	return channels.ResolveWAMentions(email, text) // Might need this in channels
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m types.RawMessage) string {
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

func scanWhatsApp(ctx context.Context, user store.User, aliases []string, language string) []int {
	email := user.Email
	var newIDs []int

	bufferCopy := channels.DefaultWAManager.PopMessages(email)
	if len(bufferCopy) == 0 {
		return nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for jidStr, msgs := range bufferCopy {
		wg.Add(1)
		go func(js string, rrms []types.RawMessage) {
			defer wg.Done()

			groupName := channels.DefaultWAManager.GetGroupName(email, js)
			msgMap := make(map[string]types.RawMessage)
			var sb strings.Builder
			for _, m := range rrms {
				msgMap[m.ID] = m
				// Resolve mentions in the text for better Gemini extraction
				resolvedText := channels.ResolveWAMentions(email, m.Text)
				sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s: %s\n", m.ID, m.Timestamp.Format("15:04"), m.Sender, resolvedText))
			}

			gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
			if err != nil {
				logger.Errorf("[SCAN-WA] Failed to init Gemini client for %s: %v", email, err)
				return
			}
			items, err := gc.Analyze(ctx, email, sb.String(), language, "whatsapp")
			if err != nil {
				logger.Errorf("[SCAN-WA] Gemini Analyze Error for %s: %v", email, err)
				return
			}

			var localMsgsToSave []store.ConsolidatedMessage
			var localNewIDs []int
			for _, item := range items {
				assignedAt := time.Now().Format(time.RFC3339)
				origText := ""
				if m, ok := msgMap[item.SourceTS]; ok {
					assignedAt = m.Timestamp.Format(time.RFC3339)
					origText = m.Text
				}

				// Check if it's 1:1 or mentioned
				is1to1 := !strings.Contains(js, "@g.us")
				isMentioned := false
				for _, alias := range aliases {
					if alias != "" && IsAliasMatched(origText, item.Requester, alias) {
						isMentioned = true
						break
					}
				}

				classification := "기타 업무"
				if is1to1 || isMentioned {
					classification = "내 업무"
				}

				assignee := item.Assignee
				if assignee == "" || assignee == "me" || assignee == "나" || assignee == "담당자" {
					if classification == "내 업무" {
						assignee = user.Name
						if assignee == "" {
							assignee = email
						}
					} else {
						// "기타 업무" 대신 빈 문자열을 넣어 UI에서 자연스럽게 보이도록 유도
						assignee = ""
					}
				}

				localMsgsToSave = append(localMsgsToSave, store.ConsolidatedMessage{
					UserEmail:    email,
					Source:       "whatsapp",
					Room:         groupName,
					Task:         item.Task,
					Requester:    item.Requester,
					Assignee:     assignee,
					AssignedAt:   assignedAt,
					SourceTS:     item.SourceTS,
					OriginalText: origText,
				})
			}

			if len(localMsgsToSave) > 0 {
				savedIDs, _ := store.SaveMessages(localMsgsToSave)
				localNewIDs = append(localNewIDs, savedIDs...)
			}

			mu.Lock()
			newIDs = append(newIDs, localNewIDs...)
			mu.Unlock()
		}(jidStr, msgs)
	}
	wg.Wait()
	return newIDs
}
