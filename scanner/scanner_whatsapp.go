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



func (s *WhatsAppScanner) processWhatsAppRoom(ctx context.Context, user store.User, aliases []string, jid string, msgs []types.RawMessage, language string) []int {
	lockKey := roomLockSvc.GetRoomKey(user.Email, "whatsapp", jid)
	lock := roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	groupName := channels.DefaultWAManager.GetGroupName(user.Email, jid)
	msgGroups := ai.GroupMessagesByTime(msgs, cfg.MessageBatchWindow)
	
	var allIDs []int
	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[WA-LOCK] AI Client Error: %v", err)
		return nil
	}

	for _, group := range msgGroups {
		if ids := s.processSingleGroup(ctx, user, aliases, jid, groupName, group, gc, language); len(ids) > 0 {
			allIDs = append(allIDs, ids...)
		}
	}
	return allIDs
}

func (s *WhatsAppScanner) processSingleGroup(ctx context.Context, user store.User, aliases []string, jid string, groupName string, group []types.RawMessage, gc *ai.GeminiClient, language string) []int {
	if len(group) == 0 {
		return nil
	}
	payload, msgMap := buildWAPayload(user, aliases, group)
	if s.isIgnorableNoise(ctx, user.Email, payload) {
		return nil
	}

	lastMsg := group[len(group)-1]
	enriched, err := EnrichWhatsAppMessage(jid, payload, lastMsg.Timestamp, &store.AliasStore{})
	if err != nil {
		logger.Errorf("[WA-SCAN] Failed to enrich message: %v", err)
		return nil
	}

	// Why: [Context Merge] Rebuilds extraction context by combining DB history and new message payload.
	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, "whatsapp", groupName)
	logger.Infof("[WA-CONTEXT] Found %d active tasks for room %s", len(tasks), groupName)

	items, err := gc.AnalyzeWithContext(ctx, user.Email, *enriched, language, "whatsapp", groupName, tasks)
	if err != nil {
		logger.Errorf("[WA-SCAN] AI Analysis Error: %v", err)
		return nil
	}

	return s.processWAItems(ctx, user, aliases, items, msgMap, groupName, !strings.Contains(jid, "@g.us"))
}

func (s *WhatsAppScanner) isIgnorableNoise(ctx context.Context, email string, payload string) bool {
	if filterSvc == nil {
		return false
	}
	isNoise, err := filterSvc.IsNoise(ctx, email, payload)
	if err != nil {
		logger.Warnf("[WA-SCAN] Filter failed for %s: %v", email, err)
		return false
	}
	return isNoise
}

func (s *WhatsAppScanner) processWAItems(ctx context.Context, user store.User, aliases []string, items []store.TodoItem, msgMap map[string]types.RawMessage, group string, is1to1 bool) []int {
	var newIDs []int
	for _, item := range items {
		if m, ok := msgMap[item.SourceTS]; ok {
			if id := saveWAItem(ctx, user, aliases, item, m, group, is1to1); id > 0 {
				newIDs = append(newIDs, id)
			}
		}
	}
	return newIDs
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
	if isFromMe(m.Sender, user) && !is1to1 {
		// Why: Identifies self-sent requests in groups as "waiting" tasks, mirroring Slack's logic for consistency.
		item.Category = string(types.CategoryTask)
	}

	params := BuildTaskParams{
		User: user, Item: item, Raw: m, Source: "whatsapp",
		Room: group, SourceChannels: []string{"whatsapp"},
	}
	msg := BuildConsolidatedMessage(params, aliases)
	id, _ := store.HandleTaskState(ctx, nil, user.Email, item, msg)
	return id
}

func isFromMe(sender string, user store.User) bool {
	lowerSender := strings.ToLower(sender)
	return lowerSender == strings.ToLower(user.Name) || lowerSender == strings.ToLower(user.Email)
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
			ids := s.processWhatsAppRoom(ctx, user, aliases, j, m, language)
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
