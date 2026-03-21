package scanner

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

	"golang.org/x/sync/errgroup"
)

func resolveWAMentions(email, text string) string {
	// WhatsApp mentions are "@12345678"
	return channels.ResolveWAMentions(email, text) // Might need this in channels
}

func scanWhatsApp(ctx context.Context, user store.User, aliases []string, language string) []int {
	email := user.Email
	var newIDs []int

	bufferCopy := channels.DefaultWAManager.PopMessages(email)
	if len(bufferCopy) == 0 {
		return nil
	}

	var mu sync.Mutex
	var eg errgroup.Group

	for jidStr, msgs := range bufferCopy {
		js, rrms := jidStr, msgs
		eg.Go(func() error {
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
				return err
			}
			items, err := gc.Analyze(ctx, email, sb.String(), language, "whatsapp")
			if err != nil {
				logger.Errorf("[SCAN-WA] Gemini Analyze Error for %s: %v", email, err)
				return err
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
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		logger.Debugf("[SCAN-WA] Partially completed with errors for %s: %v", email, err)
	}
	return newIDs
}
