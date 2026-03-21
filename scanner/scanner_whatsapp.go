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

				replyCtx := ""
				if m.ReplyToID != "" {
					replyCtx = fmt.Sprintf("[ReplyTo:%s] ", m.ReplyToID)
				}

				// [TS:m.ID] remains for backward compatibility with prompt, but adding IDs for better context
				sb.WriteString(fmt.Sprintf("[ID:%s] %s[%s] %s: %s\n", m.ID, replyCtx, m.Timestamp.Format("15:04"), m.Sender, resolvedText))
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

				checkIsMe := func(name string) bool {
					lower := strings.ToLower(name)
					if lower == "" || lower == "me" || lower == "나" || lower == "담당자" {
						return true
					}
					if strings.EqualFold(name, user.Name) || strings.EqualFold(name, email) {
						return true
					}
					for _, alias := range aliases {
						if alias != "" && strings.EqualFold(name, alias) {
							return true
						}
					}
					return false
				}

				isReqMe := checkIsMe(item.Requester)
				isAssMe := checkIsMe(item.Assignee)

				taskText := item.Task
				assignee := item.Assignee

				if isReqMe && !isAssMe {
					taskText = "[회신 대기] " + taskText // 내가 남에게 요청한 경우
				} else if isAssMe || is1to1 || isMentioned {
					assignee = user.Name
					if assignee == "" {
						assignee = email
					}
				}

				localMsgsToSave = append(localMsgsToSave, store.ConsolidatedMessage{
					UserEmail:    email,
					Source:       "whatsapp",
					Room:         groupName,
					Task:         taskText,
					Requester:    item.Requester,
					Assignee:     assignee,
					AssignedAt:   assignedAt,
					SourceTS:     item.SourceTS,
					OriginalText: origText,
					Deadline:     item.Deadline,
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
