package store

import (
	"context"
	"time"
)

func GetReport(ctx context.Context, email, start, end string) (*Report, error) {
	var r Report
	var createdAt time.Time
	var isTruncated int
	err := db.QueryRowContext(ctx, SQL.GetReport, email, start, end).Scan(
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Summary, &r.Visualization, &isTruncated, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	r.IsTruncated = isTruncated != 0
	r.CreatedAt = createdAt
	return &r, nil
}

// Why: Retrieves a specific report by its unique ID, ensuring it belongs to the requesting user.
func GetReportByID(ctx context.Context, id int, email string) (*Report, error) {
	var r Report
	var createdAt time.Time
	var isTruncated int
	err := db.QueryRowContext(ctx, SQL.GetReportByID, id, email).Scan(
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Summary, &r.Visualization, &isTruncated, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	r.IsTruncated = isTruncated != 0
	r.CreatedAt = createdAt
	return &r, nil
}

func SaveReport(ctx context.Context, r *Report) error {
	isTruncated := 0
	if r.IsTruncated {
		isTruncated = 1
	}
	_, err := db.ExecContext(ctx, SQL.UpsertReport, r.UserEmail, r.StartDate, r.EndDate, r.Summary, r.Visualization, isTruncated)
	return err
}

// Why: Provides a chronological list of a user's generated reports for the UI sidebar.
func ListReports(ctx context.Context, email string) ([]Report, error) {
	rows, err := db.QueryContext(ctx, SQL.ListReports, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []Report
	for rows.Next() {
		var r Report
		var createdAt time.Time
		var isTruncated int
		if err := rows.Scan(&r.ID, &r.StartDate, &r.EndDate, &createdAt, &isTruncated); err != nil {
			return nil, err
		}
		r.CreatedAt = createdAt
		r.IsTruncated = isTruncated != 0
		reports = append(reports, r)
	}
	return reports, nil
}

// Why: Allows users to manage their stored reports and remove unneeded entries.
func DeleteReport(ctx context.Context, id int, email string) error {
	_, err := db.ExecContext(ctx, SQL.DeleteReport, id, email)
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
