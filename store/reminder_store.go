package store

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/db"
	"time"
)

// DueSoonMessage is the row shape consumed by ReminderService.
type DueSoonMessage struct {
	ID        MessageID
	UserEmail string
	Task      string
	Deadline  string
	Metadata  string // raw JSON string; may be ""
	Room      string
	Source    string
}

// SelectDueSoon returns messages whose deadline falls within [windowStart, windowEnd].
// windowStart/End are RFC3339-formatted strings to match the deadline column format.
func SelectDueSoon(ctx context.Context, windowStart, windowEnd string) ([]DueSoonMessage, error) {
	rows, err := db.New(GetDB()).SelectDueSoonMessages(ctx, db.SelectDueSoonMessagesParams{
		Deadline:   nullString(windowStart),
		Deadline_2: nullString(windowEnd),
	})
	if err != nil {
		return nil, fmt.Errorf("select due soon: %w", err)
	}
	out := make([]DueSoonMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, DueSoonMessage{
			ID:        MessageID(r.ID),
			UserEmail: r.UserEmail,
			Task:      r.Task,
			Deadline:  r.Deadline,
			Metadata:  r.Metadata,
			Room:      r.Room,
			Source:    r.Source,
		})
	}
	return out, nil
}

// HasReminded checks if metadata JSON has a non-empty key reminded_at_<window>.
// window is "24h" or "1h".
func HasReminded(metadata, window string) bool {
	if metadata == "" {
		return false
	}
	var m map[string]any // any 사유: JSON 값 타입이 불특정 — 키 조회 후 string으로 단언
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return false
	}
	v, ok := m["reminded_at_"+window]
	if !ok {
		return false
	}
	s, _ := v.(string)
	return s != ""
}

// MarkReminded merges reminded_at_<window>=sentAt.RFC3339 into currentMetadata and persists.
// Why: caller (ReminderService) already holds the metadata string, so a re-query is unnecessary.
func MarkReminded(ctx context.Context, email string, id MessageID, currentMetadata, window string, sentAt time.Time) error {
	var m map[string]any // any 사유: 기존 JSON 필드 타입 보존을 위해 map[string]any 사용
	if currentMetadata == "" {
		m = map[string]any{}
	} else if err := json.Unmarshal([]byte(currentMetadata), &m); err != nil {
		m = map[string]any{}
	}
	m["reminded_at_"+window] = sentAt.UTC().Format(time.RFC3339)
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := db.New(GetDB()).UpdateMessageMetadataByID(ctx, db.UpdateMessageMetadataByIDParams{
		Metadata:  nullString(string(b)),
		ID:        int64(id),
		UserEmail: nullString(email),
	}); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	InvalidateCache(email)
	return nil
}
