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
	"message-consolidator/ai/core"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

// ChannelAdapter — 단일 channel 단위(WhatsApp/Telegram/Slack)로 작용하는 polymorphism 계약.
// 메서드 9개 사유: 공유 드라이버(scanChannel/processChannelRoom/processChannelGroup) 3단계가 각 단계마다
// 채널별 식별/조회/payload 빌드 메서드를 혼용해서 호출하므로 reader/writer 등으로 분할하면 모든 구현체가
// 결국 합쳐진 슈퍼셋을 구현하게 되어 분할이 실효 없음.
type ChannelAdapter interface {
	Source() string
	LogPrefix() string
	PopMessages(email string) map[string][]types.RawMessage
	GetGroupName(email, roomKey string) string
	Is1To1(roomKey string) bool
	BuildPayload(user store.User, aliases []string, msgs []types.RawMessage) (string, map[string]types.RawMessage)
	Enrich(roomKey, payload string, ts time.Time) (*types.EnrichedMessage, error)
	IsFromMe(m types.RawMessage, user store.User) bool
	Mentions(m types.RawMessage) []string
}

// driverCompletionOptOut — optional: an adapter whose drain phase already feeds
// the completion pipeline over ALL raw rows (pre-classification) implements this
// so the driver does not double-dispatch the classified subset (LINE).
type driverCompletionOptOut interface{ ownsCompletionDispatch() }

// saveThreadAnchor — optional: channels that anchor reply threads on the saved
// message's own ID (LINE) or parent thread ts (Slack) provide the thread_id
// persisted with the task; WhatsApp/Telegram leave it empty.
type saveThreadAnchor interface {
	SaveThreadID(m types.RawMessage) string
}

// saveLinker — optional: channels with permalinks (Slack) build the task's Link;
// Slack additionally registers the thread for sweep tracking inside this call.
type saveLinker interface {
	SaveLink(ctx context.Context, m types.RawMessage, email string) string
}

func scanChannel(ctx context.Context, user store.User, aliases []string, language string, wg *sync.WaitGroup, adapter ChannelAdapter) []store.MessageID {
	buffer := adapter.PopMessages(user.Email)
	if len(buffer) == 0 {
		return nil
	}

	var mu sync.Mutex
	var newIDs []store.MessageID
	var eg errgroup.Group

	for roomKey, msgs := range buffer {
		k, m := roomKey, msgs
		eg.Go(func() error {
			defer safego.Recover("scan-" + adapter.Source() + "-" + k)
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

func processChannelRoom(ctx context.Context, user store.User, aliases []string, roomKey string, msgs []types.RawMessage, language string, wg *sync.WaitGroup, adapter ChannelAdapter) []store.MessageID {
	lockKey := deps.roomLockSvc.GetRoomKey(user.Email, adapter.Source(), roomKey)
	lock := deps.roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	groupName := adapter.GetGroupName(user.Email, roomKey)
	msgGroups := core.GroupMessagesByTime(msgs, cfg.MessageBatchWindow)

	if deps.gClient == nil {
		logger.Errorf("[SCAN] %s: deps.gClient not initialized; scanner.Init may have failed", adapter.LogPrefix())
		return nil
	}

	triggerOutgoingCompletions(ctx, msgs, user, adapter, groupName)

	var allIDs []store.MessageID
	for _, group := range msgGroups {
		ids := processChannelGroup(ctx, user, aliases, roomKey, groupName, group, deps.gClient, language, wg, adapter)
		if len(ids) > 0 {
			allIDs = append(allIDs, ids...)
		}
	}
	return allIDs
}

// dispatchKind classifies how a message feeds the completion pipeline.
type dispatchKind int

const (
	dispatchNone dispatchKind = iota
	// dispatchThreaded is a fromMe quoted reply — same-thread completion/update signal.
	dispatchThreaded
	// dispatchCrossChannel is any message (fromMe or counterparty) carrying a plain
	// completion signal outside a reply chain — confirm-first candidate matching only.
	dispatchCrossChannel
)

// completionDispatchKind classifies a raw message for the outgoing-completion pipeline:
// a fromMe quoted reply is threaded (ProcessPotentialCompletion); any message carrying
// a completion signal is cross-channel (ProcessCrossChannelSignal, confirm-first only).
func completionDispatchKind(m types.RawMessage, user store.User, adapter ChannelAdapter) dispatchKind {
	if adapter.IsFromMe(m, user) && m.ReplyToID != "" {
		return dispatchThreaded
	}
	if services.HasCompletionSignal(m.Text) {
		return dispatchCrossChannel
	}
	return dispatchNone
}

// triggerOutgoingCompletions feeds the async completion pipeline when the user
// themselves reply/quote in the given room, plus any signal-bearing plain message
// for cross-channel candidate matching — mirrors the pre-refactor per-channel loop.
// Why: WithoutCancel preserves the WhaTap trace context (carried as a value) while
// detaching cancellation so the goroutines outlive the parent scan timeout.
func triggerOutgoingCompletions(ctx context.Context, msgs []types.RawMessage, user store.User, adapter ChannelAdapter, groupName string) {
	if deps.completionSvc == nil {
		return
	}
	if _, ok := adapter.(driverCompletionOptOut); ok {
		return
	}
	asyncCtx := context.WithoutCancel(ctx)
	var crossChannel []types.RawMessage
	for _, m := range msgs {
		switch completionDispatchKind(m, user, adapter) {
		case dispatchThreaded:
			dispatchThreadedCompletion(asyncCtx, m, user.Email, adapter.Source(), groupName)
		case dispatchCrossChannel:
			crossChannel = append(crossChannel, m)
		}
	}
	if len(crossChannel) > 0 {
		dispatchCrossChannelCompletions(asyncCtx, crossChannel, user, adapter, groupName)
	}
}

func dispatchThreadedCompletion(asyncCtx context.Context, m types.RawMessage, email, source, groupName string) {
	go func(em, src, room string, r types.RawMessage) {
		defer safego.Recover("outgoing-completion-" + src)
		if _, err := deps.completionSvc.ProcessPotentialCompletion(asyncCtx, store.ConsolidatedMessage{
			UserEmail: em, Source: src, Room: room, ThreadID: r.ReplyToID,
			OriginalText: r.Text, SourceTS: r.ID, CreatedAt: r.Timestamp,
			RequesterCanonical: em,
		}); err != nil {
			logger.Warnf("[SCAN] %s: outgoing completion failed for %s: %v", src, room, err)
		}
	}(email, source, groupName, m)
}

// dispatchCrossChannelCompletions feeds signal-bearing non-reply messages to the
// confirm-first cross-channel pipeline in one goroutine per room (not per message),
// bounding goroutine fan-out when a room batch has many candidate messages.
func dispatchCrossChannelCompletions(asyncCtx context.Context, msgs []types.RawMessage, user store.User, adapter ChannelAdapter, groupName string) {
	go func(em, src, room string, batch []types.RawMessage) {
		defer safego.Recover("crosschannel-completion-" + src)
		for _, r := range batch {
			env := store.ConsolidatedMessage{
				UserEmail: em, Source: src, Room: room, ThreadID: r.ReplyToID,
				OriginalText: r.Text, SourceTS: r.ID, CreatedAt: r.Timestamp,
			}
			if adapter.IsFromMe(r, user) {
				env.RequesterCanonical = em
			}
			if _, err := deps.completionSvc.ProcessCrossChannelSignal(asyncCtx, env); err != nil {
				logger.Warnf("[SCAN] %s: cross-channel completion failed for %s: %v", src, room, err)
			}
		}
	}(user.Email, adapter.Source(), groupName, msgs)
}

func processChannelGroup(ctx context.Context, user store.User, aliases []string, roomKey, groupName string, group []types.RawMessage, gc *ai.GeminiClient, language string, wg *sync.WaitGroup, adapter ChannelAdapter) []store.MessageID {
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
		logger.Errorf("[SCAN] %s: enrichment failed: %v", prefix, err)
		return nil
	}
	if adapter.Is1To1(roomKey) {
		enriched.ChatType = "1to1"
	} else {
		enriched.ChatType = "group"
	}

	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, source, groupName)
	logger.Debugf("[SCAN] %s: found %d active tasks for room %s", prefix, len(tasks), groupName)

	candidates, err := gc.AnalyzeWithContext(ctx, user.Email, *enriched, language, source, groupName, tasks)
	if err != nil {
		logger.Errorf("[SCAN] %s: AI analysis error: %v", prefix, err)
		candidates = buildEnvelopeFallbackCandidates(user, aliases, source, groupName, group, adapter)
		if len(candidates) == 0 {
			return nil
		}
		logger.Infof("[SCAN] %s: AI unavailable, envelope fallback produced %d items", prefix, len(candidates))
	}

	// Why: inject thread context so findMatch can guard against cross-thread merges,
	// and sender identity so resolve routing can distinguish auto-close (own reply)
	// from confirm-first (counterparty message).
	for i := range candidates {
		if raw, ok := msgMap[candidates[i].SourceTS]; ok {
			candidates[i].ThreadID = raw.ThreadID
			candidates[i].IsFromMe = adapter.IsFromMe(raw, user)
		}
	}

	items := deps.tasksSvc.ResolveProposals(ctx, user.Email, groupName, candidates, tasks)
	return processChannelItems(ctx, user, aliases, items, msgMap, groupName, adapter.Is1To1(roomKey), wg, adapter)
}

func isIgnorableChannelNoise(ctx context.Context, email, source, payload, prefix string) bool {
	if deps.filterSvc == nil {
		return false
	}
	isNoise, err := deps.filterSvc.IsNoise(ctx, email, source, payload)
	if err != nil {
		logger.Warnf("[SCAN] %s: filter failed for %s: %v", prefix, email, err)
		return false
	}
	return isNoise
}

func processChannelItems(ctx context.Context, user store.User, aliases []string, items []store.TodoItem, msgMap map[string]types.RawMessage, group string, is1to1 bool, wg *sync.WaitGroup, adapter ChannelAdapter) []store.MessageID {
	var newIDs []store.MessageID
	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			continue
		}
		if id := saveChannelItem(ctx, user, aliases, item, m, group, is1to1, adapter); id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
	return newIDs
}

func saveChannelItem(ctx context.Context, user store.User, aliases []string, item store.TodoItem, m types.RawMessage, group string, is1to1 bool, adapter ChannelAdapter) store.MessageID {
	if adapter.IsFromMe(m, user) && !is1to1 {
		item.Category = string(types.CategoryTask)
	}
	params, guard := services.ApplyExtractionGuard(ctx, buildChannelTaskParams(ctx, user, aliases, item, m, group, adapter))
	if !guard.Kept {
		logger.Infof("[SCAN] room=%s: extraction guard dropped item (%s)", group, guard.DropReason)
		return 0
	}
	if len(guard.Demotions) > 0 {
		logger.Debugf("[SCAN] room=%s: extraction guard demotions: %v", group, guard.Demotions)
	}
	msg := services.BuildTask(ctx, params)

	id, _ := services.HandleTaskState(ctx, nil, user.Email, params.Item, msg)
	return id
}

// senderRawFor resolves the display-name fallback shared by AI-driven and envelope-fallback
// task params. Why: Telegram의 m.Sender는 숫자 ID(예 "123456789"), m.SenderName이 표시명.
// WhatsApp은 m.Sender에 PushName/JID, m.SenderName 빈 칸. SenderName 우선 → Sender 폴백.
func senderRawFor(m types.RawMessage) string {
	if m.SenderName != "" {
		return m.SenderName
	}
	return m.Sender
}

// buildEnvelopeFallbackCandidates builds deterministic envelope-only task candidates for
// each raw message in the group when AI analysis errored out. Why: an AI outage must not
// silently drop the whole batch -- only messages that explicitly address the current user
// qualify (EnvelopeFallbackItem), so unrelated group chatter is never surfaced.
func buildEnvelopeFallbackCandidates(user store.User, aliases []string, source, groupName string, group []types.RawMessage, adapter ChannelAdapter) []store.TodoItem {
	var out []store.TodoItem
	for _, m := range group {
		params := services.TaskBuildParams{
			UserEmail:        user.Email,
			User:             user,
			Aliases:          aliases,
			SenderRaw:        senderRawFor(m),
			Source:           source,
			Room:             groupName,
			SourceTS:         m.ID,
			Timestamp:        m.Timestamp,
			OriginalText:     m.Text,
			ExplicitMentions: adapter.Mentions(m),
		}
		if item, ok := services.EnvelopeFallbackItem(params); ok {
			out = append(out, item)
		}
	}
	return out
}

func buildChannelTaskParams(ctx context.Context, user store.User, aliases []string, item store.TodoItem, m types.RawMessage, group string, adapter ChannelAdapter) services.TaskBuildParams {
	source := adapter.Source()

	params := services.TaskBuildParams{
		UserEmail:        user.Email,
		User:             user,
		Aliases:          aliases,
		Item:             item,
		SenderRaw:        senderRawFor(m),
		Source:           source,
		Room:             group,
		SourceTS:         m.ID,
		Timestamp:        m.Timestamp,
		OriginalText:     m.Text,
		RepliedToID:      m.ReplyToID,
		SourceChannels:   []string{source},
		ExplicitMentions: adapter.Mentions(m),
	}
	if anchor, ok := adapter.(saveThreadAnchor); ok {
		params.ThreadID = anchor.SaveThreadID(m)
	}
	if linker, ok := adapter.(saveLinker); ok {
		params.Link = linker.SaveLink(ctx, m, user.Email)
	}
	return params
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
