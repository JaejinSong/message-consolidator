package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
	"sync"
	"time"
)

// DigestTask holds a single task entry for the daily digest.
type DigestTask struct {
	ID          int64
	Task        string
	Source      string
	Room        string
	CreatedAt   time.Time
	Deadline    sql.NullString
	Counterpart string // received tasks: requester / assigned tasks: assignee
}

// DigestSnapshot is the result of GetDailyDigest.
type DigestSnapshot struct {
	Received      []DigestTask
	Assigned      []DigestTask
	ReceivedTotal int
	AssignedTotal int
}

// GetDailyDigest fetches pending tasks for the digest email.
// limit ≤ 0 defaults to 23.
func GetDailyDigest(ctx context.Context, email string, limit int) (DigestSnapshot, error) {
	if limit <= 0 {
		limit = 23
	}
	q := db.New(GetDB())

	// Resolve canonical user name the same way stats_store does.
	userName, _ := q.GetUserByEmailSimple(ctx, nullString(email))

	var snap DigestSnapshot
	var mu sync.Mutex
	var wg sync.WaitGroup

	runStatsQuery(&wg, func() {
		rows, err := q.ListPendingMe(ctx, db.ListPendingMeParams{
			Column1: email,
			Column2: userName,
			Column3: int64(limit),
		})
		if err != nil {
			return
		}
		tasks := make([]DigestTask, 0, len(rows))
		for _, r := range rows {
			tasks = append(tasks, DigestTask{
				ID:          r.ID,
				Task:        r.Task,
				Source:      r.Source,
				Room:        r.Room,
				CreatedAt:   nullTimeToTime(r.CreatedAt),
				Deadline:    stringToNullString(r.Deadline),
				Counterpart: r.Requester,
			})
		}
		mu.Lock()
		snap.Received = tasks
		mu.Unlock()
	})

	runStatsQuery(&wg, func() {
		rows, err := q.ListPendingOthers(ctx, db.ListPendingOthersParams{
			UserEmail: email,
			Assignee:  userName,
			Limit:     int64(limit),
		})
		if err != nil {
			return
		}
		tasks := make([]DigestTask, 0, len(rows))
		for _, r := range rows {
			tasks = append(tasks, DigestTask{
				ID:          r.ID,
				Task:        r.Task,
				Source:      r.Source,
				Room:        r.Room,
				CreatedAt:   nullTimeToTime(r.CreatedAt),
				Deadline:    stringToNullString(r.Deadline),
				Counterpart: r.Assignee,
			})
		}
		mu.Lock()
		snap.Assigned = tasks
		mu.Unlock()
	})

	runStatsQuery(&wg, func() {
		count, err := q.GetPendingMe(ctx, db.GetPendingMeParams{
			Column1: email,
			Column2: userName,
		})
		if err != nil {
			return
		}
		mu.Lock()
		snap.ReceivedTotal = int(count)
		mu.Unlock()
	})

	runStatsQuery(&wg, func() {
		count, err := q.GetPendingOthers(ctx, db.GetPendingOthersParams{
			UserEmail: email,
			Assignee:  userName,
		})
		if err != nil {
			return
		}
		mu.Lock()
		snap.AssignedTotal = int(count)
		mu.Unlock()
	})

	wg.Wait()
	return snap, nil
}

func nullTimeToTime(nt sql.NullTime) time.Time {
	if nt.Valid {
		return nt.Time
	}
	return time.Time{}
}

// stringToNullString converts a plain string to sql.NullString.
// Empty string is treated as null for optional fields like deadline.
func stringToNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
