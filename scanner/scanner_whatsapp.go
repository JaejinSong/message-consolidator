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

			for _, m := range rrms {
				msgMap[m.ID] = m
				if checkIsMe(m.Sender) && completionSvc != nil && m.ReplyToID != "" {
					completionSvc.ProcessPotentialCompletion(ctx, store.ConsolidatedMessage{
						UserEmail:    email, Source:       "whatsapp", ThreadID:     m.ReplyToID,
						OriginalText: m.Text, SourceTS:     m.ID,
					})
				}
				
				resolvedText := channels.ResolveWAMentions(email, m.Text)
				replyCtx := ""
				if m.ReplyToID != "" {
					replyCtx = fmt.Sprintf("[ReplyTo:%s] ", m.ReplyToID)
					if m.RepliedToUser != "" {
						replyCtx += fmt.Sprintf("[ReplyToUser:%s] ", m.RepliedToUser)
					}
				}
				sb.WriteString(fmt.Sprintf("[ID:%s] %s[%s] %s: %s\n", m.ID, replyCtx, m.Timestamp.Format("15:04"), m.Sender, resolvedText))
			}

			gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
			if err != nil {
				return err
			}
			items, err := gc.Analyze(ctx, email, sb.String(), language, "whatsapp", groupName)
			if err != nil {
				return err
			}

			var localNewIDs []int
			for _, item := range items {
				m, ok := msgMap[item.SourceTS]
				if !ok {
					continue
				}

				is1to1 := !strings.Contains(js, "@g.us")
				isMentioned := false
				for _, alias := range aliases {
					if alias != "" && IsAliasMatched(m.Text, item.Requester, alias) {
						isMentioned = true
						break
					}
				}

				assignee := item.Assignee
				category := item.Category
				if checkIsMe(item.Requester) && !checkIsMe(item.Assignee) {
					category = "waiting"
				} else if checkIsMe(item.Assignee) || is1to1 || isMentioned {
					assignee = user.Name
					if assignee == "" { assignee = email }
				}

				msg := store.ConsolidatedMessage{
					UserEmail: email, Source: "whatsapp", Room: groupName, Task: item.Task,
					Requester: item.Requester, Assignee: assignee, AssigneeReason: item.AssigneeReason,
					AssignedAt: m.Timestamp, SourceTS: item.SourceTS, OriginalText: m.Text,
					Deadline: item.Deadline, Category: category, ThreadID: m.ReplyToID,
					RepliedToID: m.ReplyToID, Metadata: item.Metadata,
				}

				id, err := store.HandleTaskState(email, item, msg)
				if err == nil && id > 0 { localNewIDs = append(localNewIDs, id) }
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
