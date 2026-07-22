package store

import (
	"context"
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

// UndatedCommitment is a PROMISE or WAITING row with no deadline, consumed by DispatchUndated.
type UndatedCommitment struct {
	ID                 MessageID
	UserEmail          string
	Task               string
	Requester          string
	Assignee           string
	RequesterCanonical string
	AssigneeCanonical  string
	Category           string
	Metadata           string
	Room               string
	Source             string
	Link               string
	CreatedAt          time.Time
}

// SelectUndated returns all open PROMISE/WAITING rows with no deadline across all users.
func SelectUndated(ctx context.Context) ([]UndatedCommitment, error) {
	rows, err := db.New(GetDB()).SelectUndatedCommitments(ctx)
	if err != nil {
		return nil, fmt.Errorf("select undated commitments: %w", err)
	}
	out := make([]UndatedCommitment, 0, len(rows))
	for _, r := range rows {
		var t time.Time
		if r.CreatedAt.Valid {
			t = r.CreatedAt.Time
		}
		out = append(out, UndatedCommitment{
			ID:                 MessageID(r.ID),
			UserEmail:          r.UserEmail,
			Task:               r.Task,
			Requester:          r.Requester,
			Assignee:           r.Assignee,
			RequesterCanonical: r.RequesterCanonical,
			AssigneeCanonical:  r.AssigneeCanonical,
			Category:           r.Category,
			Metadata:           r.Metadata,
			Room:               r.Room,
			Source:             r.Source,
			Link:               r.Link,
			CreatedAt:          t,
		})
	}
	return out, nil
}

// HasReminded checks if metadata JSON has a non-empty key reminded_at_<window>.
// window is "24h" or "1h".
func HasReminded(metadata, window string) bool {
	return ParseMetadata(metadata).String(metaKeyReminded(window)) != ""
}

// MarkReminded merges reminded_at_<window>=sentAt.RFC3339 into currentMetadata and persists.
// Why: caller (ReminderService) already holds the metadata string, so a re-query is unnecessary.
func MarkReminded(ctx context.Context, email string, id MessageID, currentMetadata, window string, sentAt time.Time) error {
	md := ParseMetadata(currentMetadata)
	if err := md.Set(metaKeyReminded(window), sentAt.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	b, err := md.Marshal()
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := db.New(GetDB()).UpdateMessageMetadataByID(ctx, db.UpdateMessageMetadataByIDParams{
		Metadata:  nullString(b),
		ID:        int64(id),
		UserEmail: nullString(email),
	}); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	InvalidateCache(email)
	return nil
}
