package store

import (
	"context"
	"time"
)

// Why: Retrieves a stored report by user email and date range to avoid redundant AI processing and costs.
func GetReport(ctx context.Context, email, start, end string) (*Report, error) {
	var r Report
	var createdAt time.Time
	err := db.QueryRowContext(ctx, SQL.GetReport, email, start, end).Scan(
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Summary, &r.Visualization, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	r.CreatedAt = createdAt
	return &r, nil
}

// Why: Persists an AI-generated report to the database for future retrieval and caching.
func SaveReport(ctx context.Context, r *Report) error {
	_, err := db.ExecContext(ctx, SQL.UpsertReport, r.UserEmail, r.StartDate, r.EndDate, r.Summary, r.Visualization)
	return err
}

// Why: Fetches all active messages for a user within a specified date range to provide raw material for AI summarization and network graph generation.
func GetMessagesForReport(ctx context.Context, email string, since time.Time) ([]ConsolidatedMessage, error) {
	sinceStr := since.Format("2006-01-02 15:04:05")
	rows, err := db.QueryContext(ctx, SQL.GetMessagesForReport, email, sinceStr, sinceStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
