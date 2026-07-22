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

	dispatchLineCrossChannelCompletions(ctx, chatID, rows, bundles)

	for email, msgs := range candidates {
		b := findBundle(bundles, email)
		if b == nil {
			continue
		}
		// Why: roomName derived here so analyze, tasks query, and save all use the same key.
		roomName := resolveLINERoom(chatID, chatType, msgs)
		scanChannel(ctx, b.user, b.aliases, "Korean", wg, newLineAdapter(chatID, chatType, roomName, msgs))
	}
}

// lineAdapter feeds one chat's classified rows for one user through the shared
// channel driver. Unlike WhatsApp/Telegram it is built per (chat, user) in the
// drain phase because LINE rows arrive via the line_inbox table, not a live
// per-user buffer.
type lineAdapter struct {
	chatID   string
	chatType string
	roomName string
	rows     []db.LineInbox
	rowByID  map[string]db.LineInbox
	consumed bool
}

func newLineAdapter(chatID, chatType, roomName string, rows []db.LineInbox) *lineAdapter {
	byID := make(map[string]db.LineInbox, len(rows))
	for _, r := range rows {
		byID[r.LineMessageID] = r
	}
	return &lineAdapter{chatID: chatID, chatType: chatType, roomName: roomName, rows: rows, rowByID: byID}
}

func (a *lineAdapter) Source() string    { return store.SourceLine }
func (a *lineAdapter) LogPrefix() string { return "LINE" }

func (a *lineAdapter) PopMessages(string) map[string][]types.RawMessage {
	if a.consumed {
		return nil
	}
	a.consumed = true
	raws := make([]types.RawMessage, 0, len(a.rows))
	for _, r := range a.rows {
		raws = append(raws, lineRowToRaw(r))
	}
	return map[string][]types.RawMessage{a.roomName: raws}
}

func (a *lineAdapter) GetGroupName(string, string) string { return a.roomName }

func (a *lineAdapter) Is1To1(string) bool { return a.chatType != "group" && a.chatType != "room" }

// BuildPayload formats rows as an AI-readable payload with [ID:...] tags so the
// model can return a matching SourceTS per proposal.
func (a *lineAdapter) BuildPayload(_ store.User, _ []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage, len(msgs))
	for _, m := range msgs {
		msgMap[m.ID] = m
		r := a.rowByID[m.ID]
		sender := r.SenderName
		if sender == "" {
			sender = r.SenderID
		}
		if sender == "" {
			sender = "unknown"
		}
		sb.WriteString(fmt.Sprintf("[ID:%s][%s] %s: %s\n",
			r.LineMessageID, time.Unix(r.Ts, 0).Format("15:04"), sender, r.Text))
	}
	return sb.String(), msgMap
}

func (a *lineAdapter) Enrich(_, payload string, ts time.Time) (*types.EnrichedMessage, error) {
	senderName := ""
	if len(a.rows) > 0 {
		senderName = a.rows[len(a.rows)-1].SenderName
	}
	return &types.EnrichedMessage{
		RawContent:      payload,
		SourceChannel:   store.SourceLine,
		SenderName:      senderName,
		VirtualThreadID: fmt.Sprintf("line_chat_%s", a.chatID),
		Timestamp:       ts,
	}, nil
}

// IsFromMe — LINE has no reliable fromMe signal; every row is a counterparty message.
func (a *lineAdapter) IsFromMe(types.RawMessage, store.User) bool { return false }

func (a *lineAdapter) Mentions(m types.RawMessage) []string {
	return resolveLINEMentionNames(a.rowByID[m.ID].MentionedIds)
}

// ownsCompletionDispatch — dispatchLineCrossChannelCompletions already covers ALL
// raw rows pre-classification; the driver must not re-dispatch the classified subset.
func (a *lineAdapter) ownsCompletionDispatch() {}

// SaveThreadID — LINE anchors reply threads on the message's own ID so a later
// reply (ReplyToID = this ID) matches the stored task's thread.
func (a *lineAdapter) SaveThreadID(m types.RawMessage) string { return m.ID }

func lineRowToRaw(r db.LineInbox) types.RawMessage {
	senderName := r.SenderName
	if senderName == "" {
		senderName = channels.DefaultLineManager.ResolveSenderName(r.SenderID)
	}
	return types.RawMessage{
		ID:         r.LineMessageID,
		Sender:     r.SenderID,
		SenderName: senderName,
		Text:       r.Text,
		Timestamp:  time.Unix(r.Ts, 0),
		ReplyToID:  r.ReplyToID,
		// Why: driver injects ThreadID into AI proposals from this field; LINE keys
		// reply-chain context on ReplyToID (the save-side anchor is SaveThreadID).
		ThreadID: r.ReplyToID,
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

// dispatchLineCrossChannelCompletions feeds signal-bearing LINE messages to the
// confirm-first cross-channel pipeline, one goroutine per chat (mirrors the
// channel_adapter/Slack dispatch pattern). LINE has no reliable fromMe signal, so
// every dispatched message is treated as a counterparty candidate (RequesterCanonical unset).
func dispatchLineCrossChannelCompletions(ctx context.Context, chatID string, rows []db.LineInbox, bundles []userBundle) {
	if deps.completionSvc == nil || len(rows) == 0 {
		return
	}
	var signalRows []db.LineInbox
	for _, r := range rows {
		if services.HasCompletionSignal(r.Text) {
			signalRows = append(signalRows, r)
		}
	}
	if len(signalRows) == 0 {
		return
	}
	asyncCtx := context.WithoutCancel(ctx)
	go func(bgCtx context.Context, chat string, sigRows []db.LineInbox, users []userBundle) {
		defer safego.Recover("line-crosschannel-completion-" + chat)
		for _, b := range users {
			for _, r := range sigRows {
				env := store.ConsolidatedMessage{
					UserEmail: b.user.Email, Source: store.SourceLine,
					ThreadID: r.ReplyToID, OriginalText: r.Text, SourceTS: r.LineMessageID,
					CreatedAt: time.Unix(r.Ts, 0),
				}
				if _, err := deps.completionSvc.ProcessCrossChannelSignal(bgCtx, env); err != nil {
					logger.Warnf("[LINE] cross-channel completion failed for %s: %v", b.user.Email, err)
				}
			}
		}
	}(asyncCtx, chatID, signalRows, bundles)
}

func findBundle(bundles []userBundle, email string) *userBundle {
	for i := range bundles {
		if bundles[i].user.Email == email {
			return &bundles[i]
		}
	}
	return nil
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
