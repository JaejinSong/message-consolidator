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
	waTypes "go.mau.fi/whatsmeow/types"
)



func (s *WhatsAppScanner) processWhatsAppGroup(ctx context.Context, user store.User, aliases []string, jid string, msgs []types.RawMessage, language string) []int {
	groupName := channels.DefaultWAManager.GetGroupName(user.Email, jid)
	
	// Why: [Time-Topic Hybrid] Groups messages by sender and time proximity to provide better context.
	msgGroups := ai.GroupMessagesByTime(msgs, cfg.MessageBatchWindow)
	
	var allNewIDs []int
	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[WA-SCAN] Failed to create Gemini client: %v", err)
		return nil
	}

	for _, group := range msgGroups {
		if len(group) == 0 {
			continue
		}
		
		payload, msgMap := buildWAPayload(user, aliases, group)
		
		// Why: [Noise Filtering] Decouples noise/greeting filtering from the main extraction logic.
		// Uses Lite Model (Flash-Lite) for efficiency and separates responsibilities.
		if filterSvc != nil {
			isNoise, err := filterSvc.IsNoise(ctx, user.Email, payload)
			if err != nil {
				logger.Warnf("[WA-SCAN] Filter service failed, falling back to full extraction: %v", err)
			} else if isNoise {
				logger.Debugf("[WA-SCAN] Skipping noise message group for %s", user.Email)
				continue
			}
		}

		lastMsg := group[len(group)-1]
		enriched, err := EnrichWhatsAppMessage(jid, payload, lastMsg.Timestamp, &store.AliasStore{})
		if err != nil {
			logger.Errorf("[WA-SCAN] Failed to enrich message group: %v", err)
			continue
		}

		items, err := gc.Analyze(ctx, user.Email, *enriched, language, "whatsapp", groupName)
		if err != nil {
			logger.Errorf("[WA-SCAN] AI analysis failed for group: %v", err)
			continue
		}

		is1to1 := !strings.Contains(jid, "@g.us")
		for _, item := range items {
			if m, ok := msgMap[item.SourceTS]; ok {
				id := saveWAItem(ctx, user, aliases, item, m, groupName, is1to1)
				if id > 0 {
					allNewIDs = append(allNewIDs, id)
				}
			}
		}
	}
	return allNewIDs
}

func buildWAPayload(user store.User, aliases []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range msgs {
		msgMap[m.ID] = m
		resolvedText := channels.ResolveWAMentions(user.Email, m.Text, m.MentionedIDs)
		metaStr := buildWAMetadataString(user.Email, m)
		sb.WriteString(fmt.Sprintf("[ID:%s]%s %s: %s\n", m.ID, metaStr, m.Sender, resolvedText))
	}
	return sb.String(), msgMap
}

func buildWAMetadataString(email string, m types.RawMessage) string {
	var tags []string
	if m.IsForwarded {
		tags = append(tags, "Forwarded")
	}
	
	//Why: Lists explicitly mentioned names in metadata to provide the AI with a 100% accurate source for 'Assignee' identification.
	if len(m.MentionedIDs) > 0 {
		var mentionNames []string
		for _, jid := range m.MentionedIDs {
			if id, _ := waTypes.ParseJID(jid); id.User != "" {
				if name := store.GetNameByWhatsAppNumber(email, id.User); name != "" {
					mentionNames = append(mentionNames, name)
				}
			}
		}
		if len(mentionNames) > 0 {
			tags = append(tags, fmt.Sprintf("Explicit-Mentions: %s", strings.Join(mentionNames, ", ")))
		} else {
			tags = append(tags, fmt.Sprintf("Mentions: %d", len(m.MentionedIDs)))
		}
	}

	var sb strings.Builder
	if len(tags) > 0 {
		sb.WriteString(fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", ")))
	}
	if len(m.AttachmentNames) > 0 {
		sb.WriteString(fmt.Sprintf(" [Files: %s]", strings.Join(m.AttachmentNames, ", ")))
	}
	return sb.String()
}

func saveWAItem(ctx context.Context, user store.User, aliases []string, item store.TodoItem, m types.RawMessage, group string, is1to1 bool) int {
	assignee := item.Assignee
	category := item.Category
	msg := store.ConsolidatedMessage{
		UserEmail: user.Email, Source: "whatsapp", Room: group, Task: item.Task,
		Requester: item.Requester, Assignee: assignee, AssignedAt: m.Timestamp,
		SourceTS: item.SourceTS, OriginalText: m.Text, Category: category,
		SourceChannels: []string{"whatsapp"}, // Initial source for the new task
	}
	id, _ := store.HandleTaskState(ctx, nil, user.Email, item, msg)
	return id
}

func scanWhatsApp(ctx context.Context, user store.User, aliases []string, language string) []int {
	buffer := channels.DefaultWAManager.PopMessages(user.Email)
	if len(buffer) == 0 {
		return nil
	}

	var mu sync.Mutex
	var newIDs []int
	var eg errgroup.Group
	s := &WhatsAppScanner{}

	for jid, msgs := range buffer {
		j, m := jid, msgs
		eg.Go(func() error {
			ids := s.processWhatsAppGroup(ctx, user, aliases, j, m, language)
			mu.Lock()
			newIDs = append(newIDs, ids...)
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	triggerAsyncTranslation(user.Email, newIDs)
	return newIDs
}

type WhatsAppScanner struct{}
