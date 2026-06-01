package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"message-consolidator/channels"
	"message-consolidator/db"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"

	"golang.org/x/sync/errgroup"
)

func runLineForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	if cfg == nil || cfg.LineChannelSecret == "" || cfg.LineChannelToken == "" {
		return
	}
	rows, err := store.GetUnprocessedLineMessages(ctx)
	if err != nil {
		logger.Errorf("[LINE] GetUnprocessedLineMessages: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	bundles := loadUsersForScan(ctx)
	if len(bundles) == 0 {
		return
	}

	chatGroups := groupByChatID(rows)
	var eg errgroup.Group
	eg.SetLimit(5)

	for chatID, msgs := range chatGroups {
		chatID, msgs := chatID, msgs
		eg.Go(func() error {
			scanCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			defer safego.Recover("scan-line-" + chatID)
			scanLineChat(scanCtx, chatID, msgs, bundles, wg)
			markAllProcessed(scanCtx, msgs)
			return nil
		})
	}
	_ = eg.Wait()

	// Why: flush scan metadata after all chats processed, mirroring WhatsApp/Telegram pattern.
	for _, b := range bundles {
		store.PersistAllScanMetadata(ctx, b.user.Email)
	}
}

// groupByChatID partitions unprocessed rows by chat_id so each chat is analyzed as a group.
func groupByChatID(rows []db.LineInbox) map[string][]db.LineInbox {
	m := make(map[string][]db.LineInbox)
	for _, r := range rows {
		m[r.ChatID] = append(m[r.ChatID], r)
	}
	return m
}

func markAllProcessed(ctx context.Context, rows []db.LineInbox) {
	for _, r := range rows {
		if err := store.MarkLineInboxProcessed(ctx, r.ID); err != nil {
			logger.Warnf("[LINE] MarkLineInboxProcessed(%d): %v", r.ID, err)
		}
	}
}

// scanLineChat fan-outs a chat's messages to all users: classify → per-user analyze.
func scanLineChat(ctx context.Context, chatID string, rows []db.LineInbox, bundles []userBundle, wg *sync.WaitGroup) {
	chatType := ""
	if len(rows) > 0 {
		chatType = rows[0].ChatType
	}

	// Build per-user candidate lists (fan-out, same as Slack pattern).
	candidates := make(map[string][]db.LineInbox)
	for _, row := range rows {
		for _, b := range bundles {
			cls := classifyLineMessage(row, &b.user, b.aliases)
			if cls == types.CategoryTask || cls == types.CategoryQuery {
				candidates[b.user.Email] = append(candidates[b.user.Email], row)
			}
		}
	}

	for email, msgs := range candidates {
		b := findBundle(bundles, email)
		if b == nil {
			continue
		}
		// Why: roomName derived here so analyze, tasks query, and save all use the same key.
		roomName := resolveLINERoom(chatID, chatType, msgs)
		analyzeAndSaveLine(ctx, b.user, b.aliases, chatID, chatType, roomName, msgs, wg)
	}
}

// resolveLINERoom converts a raw chatID to the human-readable room name stored in messages.Room.
// Must be called early and reused for GetActiveContextTasks, Analyze, ResolveProposals, and BuildTask
// so all DB reads/writes key on the same value.
func resolveLINERoom(chatID, chatType string, rows []db.LineInbox) string {
	if chatType != "user" {
		return chatID
	}
	// For 1:1 DMs, use "LINE DM: <senderName>" — resolve from first available row.
	for _, r := range rows {
		if r.SenderName != "" {
			return "LINE DM: " + r.SenderName
		}
		if r.SenderID != "" {
			name := channels.DefaultLineManager.ResolveSenderName(r.SenderID)
			return "LINE DM: " + name
		}
	}
	return "LINE DM: " + chatID
}

func findBundle(bundles []userBundle, email string) *userBundle {
	for i := range bundles {
		if bundles[i].user.Email == email {
			return &bundles[i]
		}
	}
	return nil
}

func analyzeAndSaveLine(ctx context.Context, user store.User, aliases []string, chatID, chatType, roomName string, rows []db.LineInbox, wg *sync.WaitGroup) {
	if len(rows) == 0 || gClient == nil {
		return
	}

	// Why: use roomName (not chatID) as lock key so DM tasks and group tasks key consistently
	// with what is stored in messages.room.
	lockKey := roomLockSvc.GetRoomKey(user.Email, "line", roomName)
	lock := roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	payload, rawMsgs := buildLinePayload(rows)

	// Why: noise filter runs before the expensive Gemini extraction call, same as channel_adapter.
	if isIgnorableChannelNoise(ctx, user.Email, "line", payload, "[LINE]") {
		return
	}

	lastRow := rows[len(rows)-1]
	enriched := &types.EnrichedMessage{
		RawContent:      payload,
		SourceChannel:   "line",
		SenderName:      lastRow.SenderName,
		VirtualThreadID: fmt.Sprintf("line_chat_%s", chatID),
		Timestamp:       time.Unix(lastRow.Ts, 0),
	}
	if chatType == "group" || chatType == "room" {
		enriched.ChatType = "group"
	} else {
		enriched.ChatType = "1to1"
	}

	// Why: load tasks first and pass directly to AnalyzeWithContext so Gemini has active-task
	// context and we avoid the double-query inside gClient.Analyze.
	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, "line", roomName)
	proposals, err := gClient.AnalyzeWithContext(ctx, user.Email, *enriched, "Korean", "line", roomName, tasks)
	if err != nil {
		logger.Errorf("[LINE] Gemini analyze error for %s: %v", user.Email, err)
		return
	}

	// Why: inject ThreadID from raw row into proposals so future reply-chain support works correctly.
	for i := range proposals {
		if raw, ok := rawMsgs[proposals[i].SourceTS]; ok {
			proposals[i].ThreadID = raw.ReplyToID
		}
	}

	items := tasksSvc.ResolveProposals(ctx, user.Email, roomName, proposals, tasks)

	var newIDs []store.MessageID
	for _, item := range items {
		raw, ok := rawMsgs[item.SourceTS]
		if !ok {
			continue
		}
		msg := buildLineConsolidatedMsg(ctx, item, raw, user, aliases, roomName)
		id, err := services.HandleTaskState(ctx, nil, user.Email, item, msg)
		if err == nil && id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
}

// buildLinePayload formats rows as a Gemini-readable payload with [ID:...] tags so
// Gemini can return a matching SourceTS per proposal.
func buildLinePayload(rows []db.LineInbox) (string, map[string]db.LineInbox) {
	var sb strings.Builder
	rawMsgs := make(map[string]db.LineInbox)
	for _, r := range rows {
		rawMsgs[r.LineMessageID] = r
		sender := r.SenderName
		if sender == "" {
			sender = r.SenderID
		}
		if sender == "" {
			sender = "unknown"
		}
		sb.WriteString(fmt.Sprintf("[ID:%s][%s] %s: %s\n",
			r.LineMessageID,
			time.Unix(r.Ts, 0).Format("15:04"),
			sender,
			r.Text,
		))
	}
	return sb.String(), rawMsgs
}

func buildLineConsolidatedMsg(ctx context.Context, item store.TodoItem, row db.LineInbox, user store.User, aliases []string, roomName string) store.ConsolidatedMessage {
	sender := row.SenderName
	if sender == "" {
		sender = channels.DefaultLineManager.ResolveSenderName(row.SenderID)
	}

	return services.BuildTask(ctx, services.TaskBuildParams{
		UserEmail:        user.Email,
		User:             user,
		Aliases:          aliases,
		Item:             item,
		SenderRaw:        sender,
		Source:           store.SourceLine,
		Room:             roomName,
		ThreadID:         row.LineMessageID,
		SourceTS:         row.LineMessageID,
		RepliedToID:      row.ReplyToID,
		Timestamp:        time.Unix(row.Ts, 0),
		OriginalText:     row.Text,
		SourceChannels:   []string{"line"},
		ExplicitMentions: resolveLINEMentionNames(row.MentionedIds),
	})
}

// resolveLINEMentionNames converts JSON-encoded LINE user IDs to display names.
// Why: ExplicitMentions is used for assignee inference by display name; raw user IDs never match.
func resolveLINEMentionNames(mentionedIdsJSON string) []string {
	var ids []string
	if err := json.Unmarshal([]byte(mentionedIdsJSON), &ids); err != nil || len(ids) == 0 {
		return nil
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name := channels.DefaultLineManager.ResolveSenderName(id)
		if name != "" && name != id {
			names = append(names, name)
		}
	}
	return names
}
