package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"go.mau.fi/whatsmeow/types"
)

func startBackgroundScanner() {
	infof("Background scanner started (1m interval)...")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Run initial scan
	runAllScans()

	for range ticker.C {
		runAllScans()
	}
}

func runAllScans() {
	users, err := GetAllUsers()
	if err != nil {
		errorf("Scanner Error: Failed to get users: %v", err)
		return
	}
	for _, u := range users {
		debugf("Starting background scan for: %s", u.Email)
		go scan(u.Email, "Korean") // Default language for background scan
	}
}

func scan(email string, language string) {
	infof("[SCAN] Starting message scan for %s (lang: %s)...", email, language)
	ctx := context.Background()

	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		errorf("[SCAN] Error: Failed to get user %s: %v", email, err)
		return
	}
	aliases, _ := GetUserAliases(user.ID)
	aliases = append(aliases, user.Email, user.Name)

	var newIDs []int
	var mu sync.Mutex
	var hasNewGmail bool
	var wg sync.WaitGroup

	// Slack Scan
	wg.Add(1)
	go func() {
		defer wg.Done()
		debugf("[SCAN] Starting Parallel Slack scan for %s", email)
		ids := scanSlack(ctx, user, aliases, language)
		mu.Lock()
		newIDs = append(newIDs, ids...)
		mu.Unlock()
	}()

	// WhatsApp Scan
	wg.Add(1)
	go func() {
		defer wg.Done()
		debugf("[SCAN] Starting Parallel WhatsApp scan for %s", email)
		ids := scanWhatsApp(ctx, user, aliases, language)
		mu.Lock()
		newIDs = append(newIDs, ids...)
		mu.Unlock()
	}()

	// Gmail Scan
	wg.Add(1)
	go func() {
		defer wg.Done()
		debugf("[SCAN] Starting Parallel Gmail scan for %s", email)
		res := ScanGmail(ctx, email, language)
		mu.Lock()
		if res {
			hasNewGmail = true
		}
		mu.Unlock()
	}()

	wg.Wait()

	// Refresh cache only if new messages were actually saved
	if len(newIDs) > 0 || hasNewGmail {
		infof("[SCAN] New messages found (%d from chat), refreshing cache and persisting metadata...", len(newIDs))
		
		// Proactive Translation for Chat Messages
		if len(newIDs) > 0 {
			targetLangs := []string{"English", "Indonesian", "Thai", "Korean"}
			// Proactive translation can also be parallelized by language
			var twg sync.WaitGroup
			for _, lang := range targetLangs {
				twg.Add(1)
				go func(l string) {
					defer twg.Done()
					debugf("[SCAN] Proactive translation started for %s -> %s", email, l)
					count, err := TranslateMessagesByID(ctx, email, newIDs, l)
					if err != nil {
						errorf("[SCAN] Proactive translation failed for %s (%s): %v", email, l, err)
					} else {
						debugf("[SCAN] Proactive translation finished for %s -> %s (%d messages)", email, l, count)
					}
				}(lang)
			}
			twg.Wait()
		}

		if err := RefreshCache(email); err != nil {
			errorf("Error refreshing cache for %s after scan: %v", email, err)
		}
		PersistAllScanMetadata(email)
		_ = ArchiveOldTasks()
	} else {
		debugf("[SCAN] No new messages found for %s, skipping DB interactions.", email)
	}
	infof("[SCAN] Finished message scan for %s", email)
}

func scanSlack(ctx context.Context, user *User, aliases []string, language string) []int {
	email := user.Email
	defer func() {
		if r := recover(); r != nil {
			errorf("[SCAN-SLACK] PANIC RECOVERED for %s: %v", email, r)
		}
	}()

	debugf("TRACELOG: Starting Slack scan for %s...", email)
	var newIDs []int
	sc := NewSlackClient(cfg.SlackToken)

	targetChannels := make(map[string]*slack.Channel)

	cursor := ""
	for {
		params := &slack.GetConversationsParameters{
			Types:  []string{"public_channel", "private_channel", "mpim", "im"},
			Cursor: cursor,
			Limit:  100,
		}

		channels, nextCursor, err := sc.api.GetConversations(params)
		if err != nil {
			if strings.Contains(err.Error(), "missing_scope") {
				errorf("[SCAN-SLACK] Permission error for %s: %v. Please ensure your Slack App has 'im:read' and 'mpim:read' scopes.", email, err)
				
				// Fallback: try only public and private channels if full list fails
				if len(params.Types) > 2 {
					debugf("[SCAN-SLACK] Retrying with limited channel types (public/private only)...")
					params.Types = []string{"public_channel", "private_channel"}
					continue
				}
			} else {
				errorf("[SCAN-SLACK] Error fetching rooms for %s: %v", email, err)
			}
			break
		}

		for _, channel := range channels {
			if channel.IsMember || channel.IsIM {
				targetChannels[channel.ID] = &channel
			}
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	if len(sc.userMap) == 0 {
		sc.FetchUsers()
	}

	since := time.Now().Add(-7 * 24 * time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for id, channel := range targetChannels {
		wg.Add(1)
		go func(cid string, ch slack.Channel) {
			defer wg.Done()
			
			lastTS := GetLastScan(email, "slack", cid)
			msgs, err := sc.GetMessages(cid, since, lastTS)
			if err != nil || len(msgs) == 0 {
				return
			}

			msgMap := make(map[string]RawChatMessage)
			var sb strings.Builder
			maxTS := lastTS

			for _, m := range msgs {
				msgMap[m.RawTS] = m
				sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s: %s\n", m.RawTS, m.Timestamp.Format("15:04"), m.User, m.Text))
				if m.RawTS > maxTS {
					maxTS = m.RawTS
				}
			}

			if sb.Len() > 0 {
				gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
				if err != nil {
					return
				}

				items, err := gc.Analyze(ctx, sb.String(), language, "slack")
				if err != nil {
					return
				}

				var localNewIDs []int
				for _, item := range items {
					assignedAt := originalMsgTimestamp(msgMap, item.SourceTS)
					classification := classifyMessage(ch, user, aliases, msgMap[item.SourceTS])
					link := fmt.Sprintf("https://slack.com/app_redirect?channel=%s&message_ts=%s", cid, item.SourceTS)

					assignee := item.Assignee
					if assignee == "" || assignee == "me" || assignee == "나" || assignee == "담당자" {
						assignee = classification
					}

					saved, newID, _ := SaveMessage(ConsolidatedMessage{
						UserEmail:    email,
						Source:       "slack",
						Room:         "#" + ch.Name,
						Task:         item.Task,
						Requester:    item.Requester,
						Assignee:     assignee,
						AssignedAt:   assignedAt,
						Link:         link,
						SourceTS:     item.SourceTS,
						OriginalText: item.OriginalText,
					})
					if saved {
						localNewIDs = append(localNewIDs, newID)
					}
				}

				mu.Lock()
				newIDs = append(newIDs, localNewIDs...)
				if maxTS != "" && maxTS != lastTS {
					UpdateLastScan(email, "slack", cid, maxTS)
				}
				mu.Unlock()
			}
		}(id, *channel)
	}
	wg.Wait()
	return newIDs
}

func originalMsgTimestamp(msgMap map[string]RawChatMessage, ts string) string {
	if m, ok := msgMap[ts]; ok {
		return m.Timestamp.Format(time.RFC3339)
	}
	return time.Now().Format(time.RFC3339)
}

func classifyMessage(channel slack.Channel, user *User, aliases []string, m RawChatMessage) string {
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

func scanWhatsApp(ctx context.Context, user *User, aliases []string, language string) []int {
	email := user.Email
	var newIDs []int
	
	waBufferMu.Lock()
	userBuffer, ok := waMessageBuffer[email]
	if !ok || len(userBuffer) == 0 {
		waBufferMu.Unlock()
		return nil
	}
	// Copy and clear buffer to avoid holding lock during analysis
	bufferCopy := make(map[string][]RawChatMessage)
	for jid, msgs := range userBuffer {
		if len(msgs) > 0 {
			bufferCopy[jid.String()] = msgs
		}
	}
	// Clear the user's buffer in the global map
	waMessageBuffer[email] = make(map[types.JID][]RawChatMessage)
	waBufferMu.Unlock()

	var mu sync.Mutex
	var wg sync.WaitGroup

	for jidStr, msgs := range bufferCopy {
		wg.Add(1)
		go func(js string, rrms []RawChatMessage) {
			defer wg.Done()
			
			jid, _ := types.ParseJID(js)
			groupName := GetGroupName(email, jid)
			msgMap := make(map[string]RawChatMessage)
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

				saved, newID, _ := SaveMessage(ConsolidatedMessage{
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
