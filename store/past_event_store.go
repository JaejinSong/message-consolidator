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
	ID           MessageID
	Task         string
	DeadlineDate time.Time
	Metadata     string
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
		return nil, fmt.Errorf("list past event candidates: %w", err)
	}
	out := make([]PastEventCandidateRow, 0, len(rows))
	for _, r := range rows {
		item := PastEventCandidateRow{ID: MessageID(r.ID), Task: r.Task, Metadata: r.Metadata}
		if r.DeadlineDate.Valid {
			item.DeadlineDate = r.DeadlineDate.Time
		}
		out = append(out, item)
	}
	return out, nil
}
