package main

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"go.mau.fi/whatsmeow/types"
)

func startBackgroundScanner() {
	logger.Infof("Background scanner started (59s interval for anti-resonance)...")
	ticker := time.NewTicker(59 * time.Second)
	defer ticker.Stop()

	// Run initial scan
	runAllScans()

	for range ticker.C {
		runAllScans()
	}
}

func runAllScans() {
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

func scanAllSources(user store.User, aliases []string) {
	logger.Debugf("[SCAN] Scanning for user: %s", user.Email)

	var wg sync.WaitGroup

	// 1. Gmail 스캔 (병렬 처리)
	if store.HasGmailToken(user.Email) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Debugf("[SCAN] Starting Gmail scan for %s", user.Email)
			ScanGmail(context.Background(), user.Email, "Korean")
		}()
	}

	// 2. Slack 스캔 (병렬 처리)
	if cfg.SlackToken != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Debugf("[SCAN] Starting Slack scan for %s", user.Email)
			scanSlack(user, aliases)
		}()
	}

	// 3. WhatsApp 스캔 (병렬 처리 및 누락된 호출 복구)
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Debugf("[SCAN] Starting WhatsApp scan for %s", user.Email)
		scanWhatsApp(context.Background(), user, aliases, "Korean")
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

	// Gmail
	if store.HasGmailToken(email) {
		ScanGmail(context.Background(), email, lang)
	}

	// Slack
	scanSlack(*user, aliases)

	// WhatsApp
	scanWhatsApp(context.Background(), *user, aliases, lang)
}

func scanSlack(user store.User, aliases []string) {
	if cfg.SlackToken == "" {
		return
	}
	sc := NewSlackClient(cfg.SlackToken)
	if err := sc.FetchUsers(); err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch users: %v", err)
	}

	// Use the Slack API to get all public channels
	channels, _, err := sc.api.GetConversations(&slack.GetConversationsParameters{
		Types: []string{"public_channel", "private_channel", "im", "mpim"},
	})
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch channels: %v", err)
		return
	}

	for _, channel := range channels {
		lastTS := store.GetLastScan(user.Email, "slack", channel.ID)
		msgs, err := sc.GetMessages(channel.ID, time.Now().Add(-24*time.Hour), lastTS)
		if err != nil {
			logger.Warnf("[SCAN-SLACK] Failed to fetch messages for channel %s (user: %s): %v", channel.Name, user.Email, err)
			continue
		}

		if len(msgs) == 0 {
			continue
		}

		newLastTS := lastTS
		for _, m := range msgs {
			classification := classifyMessage(channel, &user, aliases, m)
			if classification == "내 업무" {
				link := fmt.Sprintf("https://slack.com/archives/%s/p%s", channel.ID, strings.ReplaceAll(m.ID, ".", ""))
				store.SaveMessage(store.ConsolidatedMessage{
					UserEmail:    user.Email,
					Source:       "slack",
					Room:         sc.GetChannelName(channel.ID),
					Task:         m.Text,
					Requester:    m.Sender,
					Assignee:     "내 업무",
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
		store.UpdateLastScan(user.Email, "slack", channel.ID, newLastTS)
	}
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m RawMessage) string {
	// 1. DM이거나 직접 멘션된 경우 즉시 반환 (Early Return)
	if channel.IsIM || channel.IsMpIM {
		return "내 업무"
	}
	if user.SlackID != "" && strings.Contains(m.Text, "<@"+user.SlackID+">") {
		return "내 업무"
	}

	// 2. 소문자 변환을 반복문 밖에서 1번만 수행하여 성능 확보
	lowerText := strings.ToLower(m.Text)
	senderName := strings.ToLower(m.Sender)

	// 3. 본문과 발신자 확인을 하나의 루프로 통합
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		lowerAlias := strings.ToLower(alias)
		if strings.Contains(lowerText, lowerAlias) || strings.Contains(senderName, lowerAlias) {
			return "내 업무"
		}
	}

	return "기타 업무"
}

func scanWhatsApp(ctx context.Context, user store.User, aliases []string, language string) []int {
	email := user.Email
	var newIDs []int

	bufferCopy := DefaultWAManager.PopMessages(email)
	if len(bufferCopy) == 0 {
		return nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for jidStr, msgs := range bufferCopy {
		wg.Add(1)
		go func(js string, rrms []RawMessage) {
			defer wg.Done()

			jid, _ := types.ParseJID(js)
			groupName := GetGroupName(email, jid)
			msgMap := make(map[string]RawMessage)
			var sb strings.Builder
			for _, m := range rrms {
				msgMap[m.ID] = m
				sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s: %s\n", m.ID, m.Timestamp.Format("15:04"), m.Sender, m.Text))
			}

			gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
			if err != nil {
				logger.Errorf("[SCAN-WA] Failed to init Gemini client for %s: %v", email, err)
				return
			}
			items, err := gc.Analyze(ctx, email, sb.String(), language, "whatsapp")
			if err != nil {
				logger.Errorf("[SCAN-WA] Gemini Analyze Error for %s: %v", email, err)
				return
			}

			var localNewIDs []int
			for _, item := range items {
				assignedAt := time.Now().Format(time.RFC3339)
				if m, ok := msgMap[item.SourceTS]; ok {
					assignedAt = m.Timestamp.Format(time.RFC3339)
				}

				is1to1 := jid.Server == "s.whatsapp.net"
				isMentioned := false
				lowerText := strings.ToLower(item.OriginalText)
				for _, alias := range aliases {
					if alias != "" && strings.Contains(lowerText, strings.ToLower(alias)) {
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
					assignee = classification
				}

				saved, newID, _ := store.SaveMessage(store.ConsolidatedMessage{
					UserEmail:    email,
					Source:       "whatsapp",
					Room:         groupName,
					Task:         item.Task,
					Requester:    item.Requester,
					Assignee:     assignee,
					AssignedAt:   assignedAt,
					SourceTS:     item.SourceTS,
					OriginalText: item.OriginalText,
				})
				if saved {
					localNewIDs = append(localNewIDs, newID)
				}
			}

			mu.Lock()
			newIDs = append(newIDs, localNewIDs...)
			mu.Unlock()
		}(jidStr, msgs)
	}
	wg.Wait()
	return newIDs
}
