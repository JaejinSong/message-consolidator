// Why (file-wide `any` justification): Notion REST API DTOs use heterogeneous JSON
// maps; see notion_export.go for the canonical rationale.
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"message-consolidator/internal/primes"
	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/whatap/go-api/trace"
)

const (
	notionWAMaxPending    = 10_000
	notionWAMentionMaxLen = 97
	notionWATitleMaxLen   = 80
)

func notionWAMonthKey(ts time.Time) string { return ts.Format("2006-01") }
func notionWASettingKeyForMonth(month string) string {
	return "notion_wa_database_id_" + strings.ReplaceAll(month, "-", "_")
}
func notionWADBTitleForMonth(month string) string { return "WhatsApp Messages — " + month }

type waLogEntry struct {
	email   string
	chatJID string
	msg     types.RawMessage
}

// WANotionLogger logs every WhatsApp message to a Notion database as a row,
// enabling filter/sort by time, sender, chat room, and direction.
type WANotionLogger struct {
	token        string
	parentPageID string
	client       *http.Client

	// ChatNameResolver resolves a human-readable name for a chat JID.
	// Wired in main.go to channels.DefaultWAManager.GetGroupName.
	ChatNameResolver func(email, chatJID string) string

	// Why: injectable for tests; production defaults wired in NewWANotionLogger.
	getSettingFn    func(ctx context.Context, key string) (string, bool)
	upsertSettingFn func(ctx context.Context, key, value, updatedBy string) error

	mu      sync.Mutex
	pending []waLogEntry
	dbIDs   map[string]string // month (2006-01) → Notion DB UUID
}

func NewWANotionLogger(token, parentPageID string) *WANotionLogger {
	return &WANotionLogger{
		token:           token,
		parentPageID:    parentPageID,
		client:          whataphttpx.Client(),
		getSettingFn:    store.GetSettingRaw,
		upsertSettingFn: store.UpsertSetting,
		dbIDs:           make(map[string]string),
	}
}

func (w *WANotionLogger) Enabled() bool {
	return w.token != "" && w.parentPageID != ""
}

// Receive enqueues a message for the next flush. Safe to call from any goroutine.
func (w *WANotionLogger) Receive(email, chatJID string, msg types.RawMessage) {
	if !w.Enabled() {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) >= notionWAMaxPending {
		w.pending = w.pending[1:]
		logger.Warnf("[notion-wa] pending queue full, dropping oldest entry")
	}
	w.pending = append(w.pending, waLogEntry{email: email, chatJID: chatJID, msg: msg})
}

// Start runs the flush loop until ctx is cancelled, then performs a final flush.
func (w *WANotionLogger) Start(ctx context.Context) {
	if !w.Enabled() {
		return
	}
	timer := time.NewTimer(primes.Pick(primes.Seconds))
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			w.flush(ctx)
			timer.Reset(primes.Pick(primes.Seconds))
		case <-ctx.Done():
			// Why: ctx is already cancelled here; WithoutCancel lets the final flush run
			// while keeping the trace context.
			w.flush(context.WithoutCancel(ctx))
			return
		}
	}
}

func (w *WANotionLogger) flush(ctx context.Context) {
	traceCtx, _ := trace.Start(ctx, "/notion-wa-flush")
	var flushErr error
	defer func() { _ = trace.End(traceCtx, flushErr) }()

	w.mu.Lock()
	if len(w.pending) == 0 {
		w.mu.Unlock()
		return
	}
	drained := w.pending
	w.pending = nil
	w.mu.Unlock()

	// Group by month so each month's DB is ensured only once.
	byMonth := make(map[string][]waLogEntry, 2)
	for _, entry := range drained {
		month := notionWAMonthKey(entry.msg.Timestamp)
		byMonth[month] = append(byMonth[month], entry)
	}

	for month, entries := range byMonth {
		dbID, err := w.ensureDatabase(ctx, month)
		if err != nil {
			flushErr = err
			logger.Errorf("[notion-wa] failed to ensure database for %s: %v", month, err)
			w.mu.Lock()
			w.pending = append(entries, w.pending...)
			w.mu.Unlock()
			continue
		}
		for _, entry := range entries {
			if err := w.createRow(ctx, dbID, entry); err != nil {
				logger.Errorf("[notion-wa] failed to log msg %s: %v", entry.msg.ID, err)
				flushErr = err
			}
		}
	}
}

func (w *WANotionLogger) ensureDatabase(ctx context.Context, month string) (string, error) {
	w.mu.Lock()
	cached := w.dbIDs[month]
	w.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	settingKey := notionWASettingKeyForMonth(month)
	if v, ok := w.getSettingFn(ctx, settingKey); ok && v != "" {
		w.mu.Lock()
		w.dbIDs[month] = v
		w.mu.Unlock()
		return v, nil
	}

	dbID, err := w.createDatabase(ctx, month)
	if err != nil {
		return "", err
	}
	if err := w.upsertSettingFn(ctx, settingKey, dbID, "notion-wa-logger"); err != nil {
		logger.Errorf("[notion-wa] failed to persist database id for %s: %v", month, err)
	}
	w.mu.Lock()
	w.dbIDs[month] = dbID
	w.mu.Unlock()
	logger.Infof("[notion-wa] database created for %s: %s", month, dbID)
	return dbID, nil
}

func (w *WANotionLogger) createDatabase(ctx context.Context, month string) (string, error) {
	body := map[string]any{
		"parent": map[string]any{"page_id": w.parentPageID},
		"title": []map[string]any{
			{"text": map[string]any{"content": notionWADBTitleForMonth(month)}},
		},
		"properties": notionWADBSchema(),
	}
	resp, err := w.call(ctx, http.MethodPost, "/databases", body)
	if err != nil {
		return "", fmt.Errorf("notion-wa: create database: %w", err)
	}
	id, _ := resp["id"].(string)
	if id == "" {
		return "", fmt.Errorf("notion-wa: database created but no id returned")
	}
	return id, nil
}

func (w *WANotionLogger) createRow(ctx context.Context, dbID string, entry waLogEntry) error {
	chatName := entry.chatJID
	if w.ChatNameResolver != nil {
		chatName = w.ChatNameResolver(entry.email, entry.chatJID)
	}
	body := map[string]any{
		"parent":     map[string]any{"database_id": dbID},
		"properties": buildRowProperties(entry.msg, chatName),
	}
	_, err := w.call(ctx, http.MethodPost, "/pages", body)
	if err != nil {
		// Why: 404 means the DB was deleted in Notion; invalidate month cache so next flush recreates it.
		if strings.Contains(err.Error(), "404") {
			month := notionWAMonthKey(entry.msg.Timestamp)
			w.mu.Lock()
			delete(w.dbIDs, month)
			w.mu.Unlock()
			_ = w.upsertSettingFn(ctx, notionWASettingKeyForMonth(month), "", "notion-wa-logger")
		}
		return fmt.Errorf("notion-wa: create row: %w", err)
	}
	return nil
}

func (w *WANotionLogger) call(ctx context.Context, method, path string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, notionAPIBase+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Notion-Version", notionAPIVersion)
	req.Header.Set("Content-Type", "application/json")

	res, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("notion API error %d: %s", res.StatusCode, string(raw))
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("notion-wa: decode response: %w", err)
	}
	return result, nil
}

// notionWADBSchema returns the Notion database property definitions.
func notionWADBSchema() map[string]any {
	return map[string]any{
		"Title":         map[string]any{"title": map[string]any{}},
		"Timestamp":     map[string]any{"date": map[string]any{}},
		"Sender":        map[string]any{"rich_text": map[string]any{}},
		"Chat":          map[string]any{"rich_text": map[string]any{}},
		"Direction":     map[string]any{"select": map[string]any{"options": []map[string]any{{"name": "Incoming"}, {"name": "Outgoing"}}}},
		"Body":          map[string]any{"rich_text": map[string]any{}},
		"ReplyTo":       map[string]any{"rich_text": map[string]any{}},
		"HasAttachment": map[string]any{"checkbox": map[string]any{}},
		"Forwarded":     map[string]any{"checkbox": map[string]any{}},
		"Mentions":      map[string]any{"multi_select": map[string]any{}},
	}
}

// buildRowProperties maps a RawMessage to Notion page property values.
func buildRowProperties(msg types.RawMessage, chatName string) map[string]any {
	direction := "Incoming"
	if msg.IsFromMe {
		direction = "Outgoing"
	}
	return map[string]any{
		"Title":         notionWATitleProp(titleFromBody(msg.Text)),
		"Timestamp":     map[string]any{"date": map[string]any{"start": msg.Timestamp.Format(time.RFC3339)}},
		"Sender":        map[string]any{"rich_text": richText(msg.Sender)},
		"Chat":          map[string]any{"rich_text": richText(chatName)},
		"Direction":     map[string]any{"select": map[string]any{"name": direction}},
		"Body":          map[string]any{"rich_text": richText(msg.Text)},
		"ReplyTo":       map[string]any{"rich_text": richText(msg.RepliedToUser)},
		"HasAttachment": map[string]any{"checkbox": msg.HasAttachment},
		"Forwarded":     map[string]any{"checkbox": msg.IsForwarded},
		"Mentions":      map[string]any{"multi_select": mentionOptions(msg.MentionedNames)},
	}
}

func notionWATitleProp(text string) map[string]any {
	return map[string]any{"title": []map[string]any{{"text": map[string]any{"content": text}}}}
}

// titleFromBody returns at most notionWATitleMaxLen runes from text.
func titleFromBody(text string) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= notionWATitleMaxLen {
		return text
	}
	return string([]rune(text)[:notionWATitleMaxLen]) + "…"
}

// mentionOptions converts mention names to Notion multi_select option objects,
// truncating names that exceed Notion's 100-char option limit.
func mentionOptions(names []string) []map[string]any {
	opts := make([]map[string]any, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if utf8.RuneCountInString(n) > notionWAMentionMaxLen {
			n = string([]rune(n)[:notionWAMentionMaxLen])
		}
		opts = append(opts, map[string]any{"name": n})
	}
	return opts
}
