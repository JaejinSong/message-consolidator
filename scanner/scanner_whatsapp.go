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

	"golang.org/x/sync/errgroup"
)

func resolveWAMentions(email, text string) string {
	//Why: Recognizes standard WhatsApp numeric mentions ("@12345678") for resolution into contact names.
	return channels.ResolveWAMentions(email, text) //Why: Routes mention resolution to the channels package to centralize cross-platform user mapping logic.
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

				//Why: Intercepts outgoing messages from the user to automatically resolve and close open tasks that were specifically replied to.
				if m.Sender == "나" && completionSvc != nil && m.ReplyToID != "" {
					completionSvc.ProcessPotentialCompletion(ctx, store.ConsolidatedMessage{
						UserEmail:    email,
						Source:       "whatsapp",
						ThreadID:     m.ReplyToID,
						OriginalText: m.Text,
						SourceTS:     m.ID,
					})
				}

				//Why: Replaces numeric mentions with human-readable names before sending the payload to Gemini, significantly improving task extraction accuracy.
				resolvedText := channels.ResolveWAMentions(email, m.Text)

				replyCtx := ""
				if m.ReplyToID != "" {
					replyCtx = fmt.Sprintf("[ReplyTo:%s] ", m.ReplyToID)
					if m.RepliedToUser != "" {
						replyCtx += fmt.Sprintf("[ReplyToUser:%s] ", m.RepliedToUser)
					}
				}

				//Why: Maintains the legacy timestamp format for existing prompt compatibility while adding unique message IDs to support robust deduplication and context mapping.
				sb.WriteString(fmt.Sprintf("[ID:%s] %s[%s] %s: %s\n", m.ID, replyCtx, m.Timestamp.Format("15:04"), m.Sender, resolvedText))
			}

			gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
			if err != nil {
				logger.Errorf("[SCAN-WA] Failed to init Gemini client for %s: %v", email, err)
				return err
			}
			items, err := gc.Analyze(ctx, email, sb.String(), language, "whatsapp", groupName)
			if err != nil {
				logger.Errorf("[SCAN-WA] Gemini Analyze Error for %s: %v", email, err)
				return err
			}

			var localNewIDs []int
			for _, item := range items {
				m, ok := msgMap[item.SourceTS]
				if !ok {
					logger.Warnf("[WA-SCAN] Mismatch SourceTS: %s. Skipping task item: %s", item.SourceTS, item.Task)
					continue
				}

				//Why: Determines if the message is a direct communication or contains an explicit mention, which are high-signal indicators of a direct task assignment.
				is1to1 := !strings.Contains(js, "@g.us")
				isMentioned := false
				for _, alias := range aliases {
					if alias != "" && IsAliasMatched(m.Text, item.Requester, alias) {
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
				category := item.Category

				if isReqMe && !isAssMe {
					category = "waiting"
				} else if isAssMe || is1to1 || isMentioned {
					assignee = user.Name
					if assignee == "" {
						assignee = email
					}
				}

				msg := store.ConsolidatedMessage{
					UserEmail:      email,
					Source:         "whatsapp",
					Room:           groupName,
					Task:           taskText,
					Requester:      item.Requester,
					Assignee:       assignee,
					AssigneeReason: item.AssigneeReason,
					AssignedAt:     m.Timestamp,
					SourceTS:       item.SourceTS,
					OriginalText:   m.Text,
					Deadline:       item.Deadline,
					Category:       category,
					ThreadID:       m.ReplyToID,
					RepliedToID:    m.ReplyToID,
					Metadata:       item.Metadata,
				}

				id, err := store.HandleTaskState(email, item, msg)
				if err == nil && id > 0 {
					localNewIDs = append(localNewIDs, id)
				}
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
