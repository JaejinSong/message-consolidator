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

// TelegramScanner mirrors WhatsAppScanner — holds no state today, kept as a
// receiver so future helpers have a natural home.
type TelegramScanner struct{}

func (s *TelegramScanner) processTelegramRoom(ctx context.Context, user store.User, aliases []string, chatKey string, msgs []types.RawMessage, language string, wg *sync.WaitGroup) []int {
	lockKey := roomLockSvc.GetRoomKey(user.Email, "telegram", chatKey)
	lock := roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	groupName := channels.DefaultTelegramManager.GetGroupName(user.Email, chatKey)
	msgGroups := ai.GroupMessagesByTime(msgs, cfg.MessageBatchWindow)

	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[TG-LOCK] AI Client Error: %v", err)
		return nil
	}

	// Async completion transition: outgoing replies nudge the existing task pipeline.
	for _, m := range msgs {
		if isFromMe(m, user) && m.ReplyToID != "" && completionSvc != nil {
			go func(bgCtx context.Context, u store.User, raw types.RawMessage, g string) {
				completionSvc.ProcessPotentialCompletion(bgCtx, store.ConsolidatedMessage{
					UserEmail: u.Email, Source: "telegram", Room: g, ThreadID: raw.ReplyToID,
					OriginalText: raw.Text, SourceTS: raw.ID, CreatedAt: raw.Timestamp,
				})
			}(context.Background(), user, m, groupName)
		}
	}

	var allIDs []int
	for _, group := range msgGroups {
		if ids := s.processSingleGroup(ctx, user, aliases, chatKey, groupName, group, gc, language, wg); len(ids) > 0 {
			allIDs = append(allIDs, ids...)
		}
	}
	return allIDs
}

func (s *TelegramScanner) processSingleGroup(ctx context.Context, user store.User, aliases []string, chatKey, groupName string, group []types.RawMessage, gc *ai.GeminiClient, language string, wg *sync.WaitGroup) []int {
	if len(group) == 0 {
		return nil
	}
	payload, msgMap := buildTGPayload(user, group)
	if s.isIgnorableNoise(ctx, user.Email, payload) {
		return nil
	}

	lastMsg := group[len(group)-1]
	enriched, err := EnrichTelegramMessage(chatKey, payload, lastMsg.Timestamp)
	if err != nil {
		logger.Errorf("[TG-SCAN] Failed to enrich message: %v", err)
		return nil
	}

	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, "telegram", groupName)
	logger.Infof("[TG-CONTEXT] Found %d active tasks for room %s", len(tasks), groupName)

	candidates, err := gc.AnalyzeWithContext(ctx, user.Email, *enriched, language, "telegram", groupName, tasks)
	if err != nil {
		logger.Errorf("[TG-SCAN] AI Analysis Error: %v", err)
		return nil
	}

	items := tasksSvc.ResolveProposals(ctx, user.Email, groupName, candidates, tasks)
	is1to1 := strings.HasPrefix(chatKey, "tg_user_")
	return s.processTGItems(ctx, user, aliases, items, msgMap, groupName, is1to1, wg)
}

func (s *TelegramScanner) isIgnorableNoise(ctx context.Context, email string, payload string) bool {
	if filterSvc == nil {
		return false
	}
	isNoise, err := filterSvc.IsNoise(ctx, email, payload)
	if err != nil {
		logger.Warnf("[TG-SCAN] Filter failed for %s: %v", email, err)
		return false
	}
	return isNoise
}

func (s *TelegramScanner) processTGItems(ctx context.Context, user store.User, aliases []string, items []store.TodoItem, msgMap map[string]types.RawMessage, group string, is1to1 bool, wg *sync.WaitGroup) []int {
	var newIDs []int
	for _, item := range items {
		if m, ok := msgMap[item.SourceTS]; ok {
			if id := saveTGItem(ctx, user, aliases, item, m, group, is1to1); id > 0 {
				newIDs = append(newIDs, id)
			}
		}
	}
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
	return newIDs
}

func buildTGPayload(user store.User, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range msgs {
		msgMap[m.ID] = m
		meta := buildTGMetadataString(m)

		senderName := m.SenderName
		if senderName == "" {
			senderName = m.Sender
		}
		if m.IsFromMe {
			senderName = user.Name
		}

		sb.WriteString(fmt.Sprintf("[ID:%s]%s %s: %s\n", m.ID, meta, senderName, m.Text))
	}
	return sb.String(), msgMap
}

func buildTGMetadataString(m types.RawMessage) string {
	var tags []string
	if m.IsForwarded {
		tags = append(tags, "Forwarded")
	}
	if m.RepliedToUser != "" {
		tags = append(tags, fmt.Sprintf("Reply-To: %s", m.RepliedToUser))
	}

	var sb strings.Builder
	if len(tags) > 0 {
		sb.WriteString(fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", ")))
	}
	if m.HasAttachment {
		sb.WriteString(" [HasAttachment: true]")
	}
	return sb.String()
}

func saveTGItem(ctx context.Context, user store.User, aliases []string, item store.TodoItem, m types.RawMessage, group string, is1to1 bool) int {
	if isFromMe(m, user) && !is1to1 {
		item.Category = string(types.CategoryTask)
	}

	params := BuildTaskParams{
		User: user, Item: item, Raw: m, Source: "telegram",
		Room: group, SourceChannels: []string{"telegram"},
	}
	msg := BuildConsolidatedMessage(params, aliases)

	if id, _ := store.RouteTaskByStatus(ctx, nil, user.Email, item, msg); id > 0 {
		return id
	}
	id, _ := store.HandleTaskState(ctx, nil, user.Email, item, msg)
	return id
}

// scanTelegram drains the per-user Telegram buffer, processes each chat in parallel,
// and triggers async translation — mirror of scanWhatsApp.
func scanTelegram(ctx context.Context, user store.User, aliases []string, language string, wg *sync.WaitGroup) []int {
	buffer := channels.DefaultTelegramManager.PopMessages(user.Email)
	if len(buffer) == 0 {
		return nil
	}

	var mu sync.Mutex
	var newIDs []int
	var eg errgroup.Group
	s := &TelegramScanner{}

	for chatKey, msgs := range buffer {
		k, m := chatKey, msgs
		eg.Go(func() error {
			ids := s.processTelegramRoom(ctx, user, aliases, k, m, language, wg)
			mu.Lock()
			newIDs = append(newIDs, ids...)
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
	return newIDs
}
