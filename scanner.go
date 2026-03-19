package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
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
	debugf("Starting message scan for %s (lang: %s)...", email, language)
	ctx := context.Background()

	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		errorf("[SCAN] Error: Failed to get user %s: %v", email, err)
		return
	}
	aliases, _ := GetUserAliases(user.ID)
	// Include email and name as default aliases
	aliases = append(aliases, user.Email, user.Name)

	// Slack Scan
	debugf("About to call scanSlack for %s", email)
	newSlack := scanSlack(ctx, user, aliases, language)
	debugf("scanSlack finished for %s, hasNew: %v", email, newSlack)

	// WhatsApp Scan
	debugf("About to call scanWhatsApp for %s", email)
	newWA := scanWhatsApp(ctx, user, aliases, language)
	debugf("scanWhatsApp finished for %s, hasNew: %v", email, newWA)

	// Gmail Scan
	debugf("About to call ScanGmail for %s", email)
	newGmail := ScanGmail(ctx, email, language)
	debugf("ScanGmail finished for %s, hasNew: %v", email, newGmail)

	// Refresh cache only if new messages were actually saved
	if newSlack || newWA || newGmail {
		infof("[SCAN] New messages found, refreshing cache and persisting metadata...")
		if err := RefreshCache(email); err != nil {
			errorf("Error refreshing cache for %s after scan: %v", email, err)
		}
		// Persist all updated memory scan TS to DB since it's already awake
		PersistAllScanMetadata(email)
		// Piggyback: Archive old tasks only when the DB is already active
		_ = ArchiveOldTasks()
	} else {
		debugf("No new messages found for %s, skipping DB interactions.", email)
	}
}

func scanSlack(ctx context.Context, user *User, aliases []string, language string) bool {
	email := user.Email
	defer func() {
		if r := recover(); r != nil {
			errorf("[SCAN-SLACK] PANIC RECOVERED for %s: %v", email, r)
		}
	}()

	debugf("TRACELOG: Starting Slack scan for %s...", email)
	hasNew := false
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
	for id, channel := range targetChannels {
		lastTS := GetLastScan(email, "slack", id)
		msgs, err := sc.GetMessages(id, since, lastTS)
		if err != nil {
			continue
		}
		if len(msgs) == 0 {
			continue
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
				continue
			}

			items, err := gc.Analyze(ctx, sb.String(), language, "slack")
			if err != nil {
				continue
			}

			for _, item := range items {
				assignedAt := time.Now().Format(time.RFC3339)
				originalMsg, ok := msgMap[item.SourceTS]
				if ok {
					assignedAt = originalMsg.Timestamp.Format(time.RFC3339)
				}

				classification := "기타 업무"
				isDM := channel.IsIM || channel.IsMpIM
				isMentioned := false
				if user.SlackID != "" && strings.Contains(originalMsg.Text, "<@"+user.SlackID+">") {
					isMentioned = true
				}
				if !isMentioned {
					for _, alias := range aliases {
						if alias != "" && strings.Contains(strings.ToLower(originalMsg.Text), strings.ToLower(alias)) {
							isMentioned = true
							break
						}
					}
				}

				// Check if the sender is the user themselves
				if !isMentioned {
					senderName := strings.ToLower(originalMsg.User)
					for _, alias := range aliases {
						if alias != "" && strings.Contains(senderName, strings.ToLower(alias)) {
							isMentioned = true
							break
						}
					}
				}

				if isDM || isMentioned {
					classification = "내 업무"
				}

				link := fmt.Sprintf("https://slack.com/app_redirect?channel=%s&message_ts=%s", id, item.SourceTS)

					assignee := item.Assignee
					if assignee == "" || assignee == "me" || assignee == "나" || assignee == "담당자" {
						assignee = classification
					}

					saved, _ := SaveMessage(ConsolidatedMessage{
						UserEmail:    email,
						Source:       "slack",
						Room:         "#" + channel.Name,
						Task:         item.Task,
						Requester:    item.Requester,
						Assignee:     assignee,
						AssignedAt:   assignedAt,
					Link:         link,
					SourceTS:     item.SourceTS,
					OriginalText: item.OriginalText,
				})
				if saved {
					hasNew = true
				}
			}

			if maxTS != "" && maxTS != lastTS {
				UpdateLastScan(email, "slack", id, maxTS)
			}
		}
	}
	return hasNew
}

func scanWhatsApp(ctx context.Context, user *User, aliases []string, language string) bool {
	email := user.Email
	hasNew := false
	userWAClient := GetWhatsAppClient(email)
	if userWAClient == nil || !userWAClient.IsLoggedIn() {
		return false
	}

	waBufferMu.RLock()
	defer waBufferMu.RUnlock()

	userBuffer, ok := waMessageBuffer[email]
	if !ok {
		return false
	}

	for jid, msgs := range userBuffer {
		if len(msgs) == 0 {
			continue
		}

		groupName := GetGroupName(email, jid)
		msgMap := make(map[string]RawChatMessage)
		var sb strings.Builder
		for _, m := range msgs {
			toPart := ""
			if m.InteractedUser != "" {
				toPart = fmt.Sprintf(" -> %s", m.InteractedUser)
			}
			msgMap[m.RawTS] = m
			sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s%s: %s\n", m.RawTS, m.Timestamp.Format("15:04"), m.User, toPart, m.Text))
		}

		gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
		if err != nil {
			continue
		}
		items, err := gc.Analyze(ctx, sb.String(), language, "whatsapp")
		if err != nil {
			continue
		}

		for _, item := range items {
			assignedAt := item.AssignedAt
			originalMsg, ok := msgMap[item.SourceTS]
			if ok {
				assignedAt = originalMsg.Timestamp.Format(time.RFC3339)
			} else if sec, err := strconv.ParseInt(assignedAt, 10, 64); err == nil {
				assignedAt = time.Unix(sec, 0).Format(time.RFC3339)
			} else {
				assignedAt = time.Now().Format(time.RFC3339)
			}

			classification := "기타 업무"
			is1to1 := jid.Server == "s.whatsapp.net"
			isMentioned := false
			for _, alias := range aliases {
				if alias != "" && strings.Contains(strings.ToLower(originalMsg.Text), strings.ToLower(alias)) {
					isMentioned = true
					break
				}
			}

			// Check if the sender is the user themselves
			if !isMentioned {
				senderName := strings.ToLower(originalMsg.User)
				for _, alias := range aliases {
					if alias != "" && strings.Contains(senderName, strings.ToLower(alias)) {
						isMentioned = true
						break
					}
				}
			}

			if is1to1 || isMentioned {
				classification = "내 업무"
			}

			assignee := item.Assignee
			if assignee == "" || assignee == "me" || assignee == "나" || assignee == "담당자" {
				assignee = classification
			}

			saved, _ := SaveMessage(ConsolidatedMessage{
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
				hasNew = true
			}
		}
	}
	return hasNew
}
