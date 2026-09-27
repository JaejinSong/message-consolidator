package store

import (
	"context"
	"fmt"
	"time"

	"message-consolidator/db"
)

// PastEventCandidateRow is an active TASK row whose deadline_date has already passed
// and carries no existing completion candidate.
type PastEventCandidateRow struct {
	ID   MessageID
	Task string
	// Why: Turso returns DATE columns as text, so the day travels as YYYY-MM-DD rather than time.Time.
	DeadlineDay string
	Metadata    string
}

// ListPastEventCandidates returns active TASK rows for email whose deadline_date falls
// in [lookbackFloor, cutoff) and have no pending completion candidate yet.
func ListPastEventCandidates(ctx context.Context, email string, cutoff, lookbackFloor time.Time, limit int) ([]PastEventCandidateRow, error) {
	rows, err := db.New(GetDB()).ListPastEventCandidates(ctx, db.ListPastEventCandidatesParams{
		UserEmail: nullString(email),
		Date:      cutoff.Format("2006-01-02"),
		Date_2:    lookbackFloor.Format("2006-01-02"),
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("query past event candidates: %w", err)
	}
	out := make([]PastEventCandidateRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, PastEventCandidateRow{ID: MessageID(r.ID), Task: r.Task, DeadlineDay: r.DeadlineDay, Metadata: r.Metadata})
	}
	return out, nil
}
