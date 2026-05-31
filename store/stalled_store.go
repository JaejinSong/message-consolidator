package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"time"
)

const defaultStalledThresholdDays = 3

// StalledRequest is a TASK row that has had no update for at least N days.
type StalledRequest struct {
	ID                 MessageID
	UserEmail          string
	Task               string
	Requester          string
	Assignee           string
	RequesterCanonical string
	AssigneeCanonical  string
	Room               string
	Source             string
	Link               string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DaysStalled        int
}

// StalledBuckets groups stalled requests by who initiated them.
type StalledBuckets struct {
	// Mine: user is requester (delegated tasks that the assignee has not updated).
	Mine []StalledRequest
	// Observed: user is neither requester nor assignee (third-party X->Y tasks in same room).
	Observed []StalledRequest
}

// SelectStalled returns open TASK rows for userEmail that have had no activity for thresholdDays.
// Rows are bucketed into Mine (requester=user) and Observed (neither requester nor assignee).
func SelectStalled(ctx context.Context, userEmail string, thresholdDays int) (StalledBuckets, error) {
	if thresholdDays <= 0 {
		thresholdDays = defaultStalledThresholdDays
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -thresholdDays)
	rows, err := db.New(GetDB()).SelectStalledRequests(ctx, db.SelectStalledRequestsParams{
		UserEmail: userEmail,
		UpdatedAt: sql.NullTime{Time: cutoff, Valid: true},
	})
	if err != nil {
		return StalledBuckets{}, fmt.Errorf("select stalled requests: %w", err)
	}

	var buckets StalledBuckets
	for _, r := range rows {
		sr := StalledRequest{
			ID:                 MessageID(r.ID),
			UserEmail:          r.UserEmail,
			Task:               r.Task,
			Requester:          r.Requester,
			Assignee:           r.Assignee,
			RequesterCanonical: r.RequesterCanonical,
			AssigneeCanonical:  r.AssigneeCanonical,
			Room:               r.Room,
			Source:             r.Source,
			Link:               r.Link,
			DaysStalled:        int(r.DaysStalled),
		}
		if r.CreatedAt.Valid {
			sr.CreatedAt = r.CreatedAt.Time
		}
		if r.UpdatedAt.Valid {
			sr.UpdatedAt = r.UpdatedAt.Time
		}

		if r.RequesterCanonical == userEmail {
			buckets.Mine = append(buckets.Mine, sr)
		} else if r.AssigneeCanonical != userEmail {
			buckets.Observed = append(buckets.Observed, sr)
		}
	}
	return buckets, nil
}
