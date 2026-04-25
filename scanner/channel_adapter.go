// Package scanner — shared driver for real-time channel scanners (WhatsApp/Telegram).
// WhatsApp and Telegram run the same pipeline (buffer drain → per-room lock → AI batch
// extraction → item save → async translation); only payload formatting, sender
// resolution, and 1:1-vs-group detection differ per channel. ChannelAdapter is the
// seam that keeps channel-specific concerns out of the shared driver.
package scanner

import (
	"context"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

type ChannelAdapter interface {
	Source() string
	LogPrefix() string
	PopMessages(email string) map[string][]types.RawMessage
	GetGroupName(email, roomKey string) string
	Is1To1(roomKey string) bool
	BuildPayload(user store.User, aliases []string, msgs []types.RawMessage) (string, map[string]types.RawMessage)
	Enrich(roomKey, payload string, ts time.Time) (*types.EnrichedMessage, error)
}

func scanChannel(ctx context.Context, user store.User, aliases []string, language string, wg *sync.WaitGroup, adapter ChannelAdapter) []int {
	buffer := adapter.PopMessages(user.Email)
	if len(buffer) == 0 {
		return nil
	}

	var mu sync.Mutex
	var newIDs []int
	var eg errgroup.Group

	for roomKey, msgs := range buffer {
		k, m := roomKey, msgs
		eg.Go(func() error {
			ids := processChannelRoom(ctx, user, aliases, k, m, language, wg, adapter)
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

func processChannelRoom(ctx context.Context, user store.User, aliases []string, roomKey string, msgs []types.RawMessage, language string, wg *sync.WaitGroup, adapter ChannelAdapter) []int {
	lockKey := roomLockSvc.GetRoomKey(user.Email, adapter.Source(), roomKey)
	lock := roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	groupName := adapter.GetGroupName(user.Email, roomKey)
	msgGroups := ai.GroupMessagesByTime(msgs, cfg.MessageBatchWindow)

	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[%s-LOCK] AI Client Error: %v", adapter.LogPrefix(), err)
		return nil
	}

	triggerOutgoingCompletions(ctx, msgs, user, adapter.Source(), groupName)

	var allIDs []int
	for _, group := range msgGroups {
		ids := processChannelGroup(ctx, user, aliases, roomKey, groupName, group, gc, language, wg, adapter)
		if len(ids) > 0 {
			allIDs = append(allIDs, ids...)
		}
	}
	return allIDs
}

// triggerOutgoingCompletions feeds the async completion pipeline when the user
// themselves reply/quote in the given room — mirrors the pre-refactor per-channel loop.
// Why: ctx is plumbed through so WhaTap trace propagation continues into the async
// goroutine; the goroutine itself uses Background() to outlive the parent scan timeout.
func triggerOutgoingCompletions(_ context.Context, msgs []types.RawMessage, user store.User, source, groupName string) {
	if completionSvc == nil {
		return
	}
	for _, m := range msgs {
		if !isFromMe(m, user) || m.ReplyToID == "" {
			continue
		}
		raw := m
		go func(em, src, room string, r types.RawMessage) { //nolint:contextcheck // Independent completion pipeline; lifecycle decoupled from scan ctx.
			if _, err := completionSvc.ProcessPotentialCompletion(context.Background(), store.ConsolidatedMessage{
				UserEmail: em, Source: src, Room: room, ThreadID: r.ReplyToID,
				OriginalText: r.Text, SourceTS: r.ID, CreatedAt: r.Timestamp,
			}); err != nil {
				logger.Warnf("[OUTGOING-COMPLETION] %s/%s: %v", src, room, err)
			}
		}(user.Email, source, groupName, raw)
	}
}

func processChannelGroup(ctx context.Context, user store.User, aliases []string, roomKey, groupName string, group []types.RawMessage, gc *ai.GeminiClient, language string, wg *sync.WaitGroup, adapter ChannelAdapter) []int {
	if len(group) == 0 {
		return nil
	}
	prefix := adapter.LogPrefix()
	source := adapter.Source()

	payload, msgMap := adapter.BuildPayload(user, aliases, group)
	if isIgnorableChannelNoise(ctx, user.Email, source, payload, prefix) {
		return nil
	}

	enriched, err := adapter.Enrich(roomKey, payload, group[len(group)-1].Timestamp)
	if err != nil {
		logger.Errorf("[%s-SCAN] Failed to enrich message: %v", prefix, err)
		return nil
	}

	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, source, groupName)
	logger.Infof("[%s-CONTEXT] Found %d active tasks for room %s", prefix, len(tasks), groupName)

	candidates, err := gc.AnalyzeWithContext(ctx, user.Email, *enriched, language, source, groupName, tasks)
	if err != nil {
		logger.Errorf("[%s-SCAN] AI Analysis Error: %v", prefix, err)
		return nil
	}

	items := tasksSvc.ResolveProposals(ctx, user.Email, groupName, candidates, tasks)
	return processChannelItems(ctx, user, aliases, items, msgMap, groupName, adapter.Is1To1(roomKey), wg, source)
}

func isIgnorableChannelNoise(ctx context.Context, email, source, payload, prefix string) bool {
	if filterSvc == nil {
		return false
	}
	isNoise, err := filterSvc.IsNoise(ctx, email, source, payload)
	if err != nil {
		logger.Warnf("[%s-SCAN] Filter failed for %s: %v", prefix, email, err)
		return false
	}
	return isNoise
}

func processChannelItems(ctx context.Context, user store.User, aliases []string, items []store.TodoItem, msgMap map[string]types.RawMessage, group string, is1to1 bool, wg *sync.WaitGroup, source string) []int {
	var newIDs []int
	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			continue
		}
		if id := saveChannelItem(ctx, user, aliases, item, m, group, is1to1, source); id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
	return newIDs
}

func saveChannelItem(ctx context.Context, user store.User, aliases []string, item store.TodoItem, m types.RawMessage, group string, is1to1 bool, source string) int {
	if isFromMe(m, user) && !is1to1 {
		item.Category = string(types.CategoryTask)
	}

	params := BuildTaskParams{
		User: user, Item: item, Raw: m, Source: source,
		Room: group, SourceChannels: []string{source},
	}
	msg := BuildConsolidatedMessage(params, aliases)

	if id, _ := services.RouteTaskByStatus(ctx, nil, user.Email, item, msg); id > 0 {
		return id
	}
	id, _ := services.HandleTaskState(ctx, nil, user.Email, item, msg)
	return id
}

// isFromMe is shared by the WhatsApp and Telegram adapters (Slack has its own
// SlackID-aware variant inline in scanner_slack.go).
func isFromMe(m types.RawMessage, user store.User) bool {
	if m.IsFromMe {
		return true
	}
	lowerSender := strings.ToLower(m.Sender)
	return lowerSender == strings.ToLower(user.Name) || lowerSender == strings.ToLower(user.Email)
}
