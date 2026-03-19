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
	logger.Infof("Background scanner started (1m interval)...")
	ticker := time.NewTicker(1 * time.Minute)
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
}

func scanAllSources(user store.User, aliases []string) {
	logger.Debugf("[SCAN] Scanning for user: %s", user.Email)

	// Gmail scan
	if store.HasGmailToken(user.Email) {
		logger.Debugf("[SCAN] Starting Gmail scan for %s", user.Email)
		ScanGmail(context.Background(), user.Email, "Korean") // Default to Korean for background scan
	}

	// Slack scan (currently shared, but can be filtered by aliases/user)
	logger.Debugf("[SCAN] Starting Slack scan for %s", user.Email)
	scanSlack(user, aliases)

	// Persistence of scan metadata
	store.PersistAllScanMetadata(user.Email)

	// Archive old tasks
	if err := store.ArchiveOldTasks(); err != nil {
		logger.Errorf("[SCAN] Failed to archive old tasks for %s: %v", user.Email, err)
	}
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
			logger.Debugf("[SCAN-SLACK] Failed to fetch messages for %s: %v", channel.Name, err)
			continue
		}

		if len(msgs) == 0 {
			continue
		}

		newLastTS := lastTS
		for _, m := range msgs {
			classification := classifyMessage(channel, &user, aliases, m)
			if classification == "내 업무" {
				link := fmt.Sprintf("https://slack.com/archives/%s/p%s", channel.ID, strings.ReplaceAll(m.RawTS, ".", ""))
			store.SaveMessage(store.ConsolidatedMessage{
					UserEmail:    user.Email,
					Source:       "slack",
					Room:         sc.GetChannelName(channel.ID),
					Task:         m.Text,
					Requester:    m.User,
					Assignee:     "내 업무",
					AssignedAt:   m.Timestamp.Format(time.RFC3339),
					Link:         link,
					SourceTS:     m.RawTS,
					OriginalText: m.Text,
				})
			}
			if m.RawTS > newLastTS {
				newLastTS = m.RawTS
			}
		}
		store.UpdateLastScan(user.Email, "slack", channel.ID, newLastTS)
	}
}

func originalMsgTimestamp(msgMap map[string]store.RawChatMessage, ts string) string {
	if m, ok := msgMap[ts]; ok {
		return m.Timestamp.Format(time.RFC3339)
	}
	return time.Now().Format(time.RFC3339)
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m store.RawChatMessage) string {
	isDM := channel.IsIM || channel.IsMpIM
	isMentioned := false
	if user.SlackID != "" && strings.Contains(m.Text, "<@"+user.SlackID+">") {
		isMentioned = true
	}
	if !isMentioned {
		for _, alias := range aliases {
			if alias != "" && strings.Contains(strings.ToLower(m.Text), strings.ToLower(alias)) {
				isMentioned = true
				break
			}
		}
	}
	if !isMentioned {
		senderName := strings.ToLower(m.User)
		for _, alias := range aliases {
			if alias != "" && strings.Contains(senderName, strings.ToLower(alias)) {
				isMentioned = true
				break
			}
		}
	}
	if isDM || isMentioned {
		return "내 업무"
	}
	return "기타 업무"
}

func scanWhatsApp(ctx context.Context, user store.User, aliases []string, language string) []int {
	email := user.Email
	var newIDs []int
	
	waBufferMu.Lock()
	userBuffer, ok := waMessageBuffer[email]
	if !ok || len(userBuffer) == 0 {
		waBufferMu.Unlock()
		return nil
	}
	// Copy and clear buffer to avoid holding lock during analysis
	bufferCopy := make(map[string][]store.RawChatMessage)
	for jid, msgs := range userBuffer {
		if len(msgs) > 0 {
			bufferCopy[jid.String()] = msgs
		}
	}
	// Clear the user's buffer in the global map
	waMessageBuffer[email] = make(map[types.JID][]store.RawChatMessage)
	waBufferMu.Unlock()

	var mu sync.Mutex
	var wg sync.WaitGroup

	for jidStr, msgs := range bufferCopy {
		wg.Add(1)
		go func(js string, rrms []store.RawChatMessage) {
			defer wg.Done()

			jid, _ := types.ParseJID(js)
			groupName := GetGroupName(email, jid)
			msgMap := make(map[string]store.RawChatMessage)
			var sb strings.Builder
			for _, m := range rrms {
				msgMap[m.RawTS] = m
				sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s: %s\n", m.RawTS, m.Timestamp.Format("15:04"), m.User, m.Text))
			}

			gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
			if err != nil {
				return
			}
			items, err := gc.Analyze(ctx, sb.String(), language, "whatsapp")
			if err != nil {
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
