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
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Visualization, &isTruncated, &createdAt, &r.Summary,
	)
	if err != nil {
		return nil, err
	}
	r.IsTruncated = isTruncated != 0
	r.CreatedAt = createdAt
	
	// Why: Ensures all available translations are loaded for the front-end to enable seamless language switching.
	r.Translations, _ = GetReportTranslations(ctx, r.ID)
	return &r, nil
}

// Why: Retrieves a specific report by its unique ID, ensuring it belongs to the requesting user.
func GetReportByID(ctx context.Context, id int, email string) (*Report, error) {
	var r Report
	var createdAt time.Time
	var isTruncated int
	err := db.QueryRowContext(ctx, SQL.GetReportByID, id, email).Scan(
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Visualization, &isTruncated, &createdAt, &r.Summary,
	)
	if err != nil {
		return nil, err
	}
	r.IsTruncated = isTruncated != 0
	r.CreatedAt = createdAt

	// Why: Ensures all available translations are loaded for the front-end to enable seamless language switching.
	r.Translations, _ = GetReportTranslations(ctx, r.ID)
	return &r, nil
}

// Why: Saves the metadata portion of a report and returns the generated primary key.
func SaveReport(ctx context.Context, r *Report) (int64, error) {
	isTruncated := 0
	if r.IsTruncated {
		isTruncated = 1
	}
	res, err := db.ExecContext(ctx, SQL.InsertReport, r.UserEmail, r.StartDate, r.EndDate, r.Visualization, isTruncated)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Why: Persists a specific language translation for a given report metadata entry.
func SaveReportTranslation(ctx context.Context, reportID int64, langCode, summary string) error {
	_, err := db.ExecContext(ctx, SQL.InsertReportTranslation, reportID, langCode, summary)
	return err
}

// Why: Retrieves all available language translations for a specific report to support the multi-language UI.
func GetReportTranslations(ctx context.Context, reportID int) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, SQL.GetReportTranslations, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	translations := make(map[string]string)
	for rows.Next() {
		var langCode, summary string
		if err := rows.Scan(&langCode, &summary); err != nil {
			return nil, err
		}
		translations[langCode] = summary
	}
	return translations, nil
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
