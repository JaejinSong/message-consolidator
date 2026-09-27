package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"message-consolidator/db"
)

// Replay tuning constants for ListReplayableWAMessages. Why: a message stays eligible
// for up to waMaxScanAttempts attempts, is only picked up waCreatedGrace after insert
// (lets the writer settle before a concurrent scan grabs it), and a pop lock older than
// waPoppedStale is treated as abandoned (crashed worker) rather than in-flight.
const (
	waMaxScanAttempts = 3
	waCreatedGrace    = 13 * time.Minute
	waPoppedStale     = 31 * time.Minute
	waLookback        = 7 * 24 * time.Hour
	waReplayLimit     = 997
	waSQLiteInChunk   = 499
)

// sqliteDatetimeFormat matches the text SQLite's CURRENT_TIMESTAMP / datetime('now')
// produce, so string comparisons against wa_messages' TEXT/DATETIME columns are valid.
const sqliteDatetimeFormat = "2006-01-02 15:04:05"

func InsertWAMessage(ctx context.Context, arg db.InsertWAMessageParams) error {
	conn := GetDB()
	if conn == nil {
		return fmt.Errorf("wa_messages: db not initialised")
	}
	return db.New(conn).InsertWAMessage(ctx, arg)
}

// ReplayableWAMessage is a wa_messages row eligible for scan replay.
type ReplayableWAMessage struct {
	MessageID string
	ChatJID   string
	RawJSON   string
	Attempts  int64
}

// ListReplayableWAMessages returns wa_messages rows not yet processed, under the retry
// cap, past the write-settle grace period, not held by a live pop, and within the
// lookback window.
func ListReplayableWAMessages(ctx context.Context, email string, now time.Time) ([]ReplayableWAMessage, error) {
	conn := GetDB()
	if conn == nil {
		return nil, fmt.Errorf("wa_messages: db not initialised")
	}
	rows, err := db.New(conn).ListReplayableWAMessages(ctx, db.ListReplayableWAMessagesParams{
		Email:        email,
		ScanAttempts: waMaxScanAttempts,
		CreatedAt:    now.Add(-waCreatedGrace).UTC().Format(sqliteDatetimeFormat),
		PoppedAt:     sql.NullTime{Time: now.Add(-waPoppedStale).UTC(), Valid: true},
		Ts:           now.Add(-waLookback).Unix(),
		Limit:        waReplayLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list replayable wa_messages: %w", err)
	}
	out := make([]ReplayableWAMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReplayableWAMessage{
			MessageID: r.MessageID,
			ChatJID:   r.ChatJid,
			RawJSON:   r.RawJson,
			Attempts:  r.ScanAttempts,
		})
	}
	return out, nil
}

// MarkWAMessagesPopped claims ids for an in-flight scan attempt. Why: popped_at lets a
// concurrent replay skip rows another worker already picked up, without blocking on a
// DB-level lock.
func MarkWAMessagesPopped(ctx context.Context, email string, ids []string) error {
	return updateWAMessagesByID(ctx, email, ids,
		"UPDATE wa_messages SET popped_at = CURRENT_TIMESTAMP WHERE email = ? AND processed_at IS NULL AND message_id IN (%s)")
}

// MarkWAMessagesProcessed marks ids as durably consumed so they never replay again.
func MarkWAMessagesProcessed(ctx context.Context, email string, ids []string) error {
	return updateWAMessagesByID(ctx, email, ids,
		"UPDATE wa_messages SET processed_at = CURRENT_TIMESTAMP WHERE email = ? AND message_id IN (%s)")
}

// MarkWAMessagesFailed increments scan_attempts after a failed scan so ids eventually
// age out of ListReplayableWAMessages via the retry cap.
func MarkWAMessagesFailed(ctx context.Context, email string, ids []string) error {
	return updateWAMessagesByID(ctx, email, ids,
		"UPDATE wa_messages SET scan_attempts = scan_attempts + 1 WHERE email = ? AND processed_at IS NULL AND message_id IN (%s)")
}

// updateWAMessagesByID runs stmtTemplate (a single "%s" placeholder for the IN-clause
// slots) in batches of waSQLiteInChunk ids. Why: raw SQL retained here per CLAUDE.md --
// dynamic IN clause; SQLite caps bound variables per statement, so ids are chunked.
func updateWAMessagesByID(ctx context.Context, email string, ids []string, stmtTemplate string) error {
	if len(ids) == 0 {
		return nil
	}
	conn := GetDB()
	if conn == nil {
		return fmt.Errorf("wa_messages: db not initialised")
	}
	for start := 0; start < len(ids); start += waSQLiteInChunk {
		end := start + waSQLiteInChunk
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+1)
		args = append(args, email)
		for _, id := range batch {
			args = append(args, id)
		}
		stmt := fmt.Sprintf(stmtTemplate, placeholders)
		if _, err := conn.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("update wa_messages batch: %w", err)
		}
	}
	return nil
}

// ListWAMessagesParams holds optional filter values for ListWAMessages.
// Zero values mean "no filter": empty string = any text, 0 = any timestamp.
type ListWAMessagesParams struct {
	Email     string
	ChatJID   string
	Direction string
	FromTs    int64
	ToTs      int64
	Limit     int64
	Offset    int64
}

func ListWAMessages(ctx context.Context, p ListWAMessagesParams) ([]db.ListWAMessagesRow, error) {
	conn := GetDB()
	if conn == nil {
		return nil, fmt.Errorf("wa_messages: db not initialised")
	}
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	return db.New(conn).ListWAMessages(ctx, db.ListWAMessagesParams{
		Column1: p.Email,
		Column2: p.ChatJID,
		Column3: p.Direction,
		Column4: p.FromTs,
		Column5: p.ToTs,
		Limit:   p.Limit,
		Offset:  p.Offset,
	})
}
