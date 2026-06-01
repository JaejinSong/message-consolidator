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

// scanLineChat fan-outs a chat's messages across all users: classify → noise gate → Gemini → save.
func scanLineChat(ctx context.Context, chatID string, rows []db.LineInbox, bundles []userBundle, wg *sync.WaitGroup) {
	chatType := ""
	if len(rows) > 0 {
		chatType = rows[0].ChatType
	}

	// Build per-user candidate lists.
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
		analyzeAndSaveLine(ctx, b.user, b.aliases, chatID, chatType, msgs, wg)
	}
}

func findBundle(bundles []userBundle, email string) *userBundle {
	for i := range bundles {
		if bundles[i].user.Email == email {
			return &bundles[i]
		}
	}
	return nil
}

func analyzeAndSaveLine(ctx context.Context, user store.User, aliases []string, chatID, chatType string, rows []db.LineInbox, wg *sync.WaitGroup) {
	if len(rows) == 0 || gClient == nil {
		return
	}

	lockKey := roomLockSvc.GetRoomKey(user.Email, "line", chatID)
	lock := roomLockSvc.AcquireLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	payload, rawMsgs := buildLinePayload(rows)
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

	proposals, err := gClient.Analyze(ctx, user.Email, *enriched, "Korean", "line", chatID)
	if err != nil {
		logger.Errorf("[LINE] Gemini analyze error for %s: %v", user.Email, err)
		return
	}

	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), user.Email, "line", chatID)
	items := tasksSvc.ResolveProposals(ctx, user.Email, chatID, proposals, tasks)

	var newIDs []store.MessageID
	for _, item := range items {
		raw, ok := rawMsgs[item.SourceTS]
		if !ok {
			continue
		}
		msg := buildLineConsolidatedMsg(ctx, item, raw, user, aliases, chatID, chatType)
		id, err := services.HandleTaskState(ctx, nil, user.Email, item, msg)
		if err == nil && id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	triggerAsyncTranslation(ctx, user.Email, newIDs, wg)
}

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
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", time.Unix(r.Ts, 0).Format("15:04"), sender, r.Text))
	}
	return sb.String(), rawMsgs
}

func buildLineConsolidatedMsg(ctx context.Context, item store.TodoItem, row db.LineInbox, user store.User, aliases []string, chatID, chatType string) store.ConsolidatedMessage {
	sender := row.SenderName
	if sender == "" {
		sender = channels.DefaultLineManager.ResolveSenderName(row.SenderID)
	}
	roomName := chatID
	if chatType == "user" {
		roomName = "LINE DM: " + sender
	}

	return services.BuildTask(ctx, services.TaskBuildParams{
		UserEmail:      user.Email,
		User:           user,
		Aliases:        aliases,
		Item:           item,
		SenderRaw:      sender,
		Source:         store.SourceLine,
		Room:           roomName,
		ThreadID:       row.LineMessageID,
		SourceTS:       row.LineMessageID,
		Timestamp:      time.Unix(row.Ts, 0),
		OriginalText:   row.Text,
		SourceChannels: []string{"line"},
		ExplicitMentions: parseMentionedNames(row.MentionedIds),
	})
}

// parseMentionedNames extracts IDs from the JSON mentioned_ids array.
// These are LINE user IDs, not display names, but serve as a fallback mention list.
func parseMentionedNames(json string) []string {
	var ids []string
	_ = unmarshalStringSlice(json, &ids)
	return ids
}

func unmarshalStringSlice(s string, out *[]string) error {
	return json.Unmarshal([]byte(s), out)
}
