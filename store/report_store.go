package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func GetReport(ctx context.Context, email, start, end string) (*Report, error) {
	var r Report
	var createdAt time.Time
	var isTruncated int
	var summary sql.NullString
	err := db.QueryRowContext(ctx, SQL.GetReport, email, start, end).Scan(
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Visualization, &isTruncated, &createdAt, &summary,
	)
	if err != nil {
		return nil, err
	}
	r.Summary = summary.String
	r.IsTruncated = isTruncated != 0
	r.CreatedAt = createdAt

	// Why: Ensures all available translations are loaded for the front-end to enable seamless language switching.
	r.Translations, _ = GetReportTranslations(ctx, r.ID)
	return &r, nil
}

// GetReportByDate retrieves a report for a specific date (YYYY-MM-DD).
// Why: Enables exact date-based caching to avoid redundant AI generation for the same day.
func GetReportByDate(ctx context.Context, email, date string) (*Report, error) {
	var r Report
	var createdAt time.Time
	var isTruncated int
	var summary sql.NullString
	err := db.QueryRowContext(ctx, SQL.GetReportByDate, email, date, date).Scan(
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Visualization, &isTruncated, &createdAt, &summary,
	)
	if err != nil {
		return nil, err
	}
	r.Summary = summary.String
	r.IsTruncated, r.CreatedAt = isTruncated != 0, createdAt
	r.Translations, _ = GetReportTranslations(ctx, r.ID)
	return &r, nil
}

// Why: Retrieves a specific report by its unique ID, ensuring it belongs to the requesting user.
func GetReportByID(ctx context.Context, id int, email string) (*Report, error) {
	var r Report
	var createdAt time.Time
	var isTruncated int
	var summary sql.NullString
	err := db.QueryRowContext(ctx, SQL.GetReportByID, id, email).Scan(
		&r.ID, &r.UserEmail, &r.StartDate, &r.EndDate, &r.Visualization, &isTruncated, &createdAt, &summary,
	)
	if err != nil {
		return nil, err
	}
	r.Summary = summary.String
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

// GetReportTranslationsBatch retrieves translations for multiple reports in a single query to eliminate N+1 overhead.
func GetReportTranslationsBatch(ctx context.Context, reportIDs []int) (map[int]map[string]string, error) {
	if len(reportIDs) == 0 {
		return make(map[int]map[string]string), nil
	}

	placeholders := make([]string, len(reportIDs))
	args := make([]interface{}, len(reportIDs))
	for i, id := range reportIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT report_id, language_code, summary FROM report_translations WHERE report_id IN (%s)", strings.Join(placeholders, ","))
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[int]map[string]string)
	for rows.Next() {
		var rid int
		var lang, summary string
		if err := rows.Scan(&rid, &lang, &summary); err != nil {
			return nil, err
		}
		if res[rid] == nil {
			res[rid] = make(map[string]string)
		}
		res[rid][lang] = summary
	}
	return res, nil
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
// Refactored: Uses 1+1 pattern (metadata batch + translations batch) to eliminate N+1 overhead.
func ListReports(ctx context.Context, email string) ([]Report, error) {
	reports, err := fetchReportMetadata(ctx, email)
	if err != nil || len(reports) == 0 {
		return reports, err
	}

	ids := collectReportIDs(reports)
	transMap, err := GetReportTranslationsBatch(ctx, ids)
	if err != nil {
		// Why: Partial success - return reports without translations rather than failing entirely.
		return reports, nil
	}

	mapTranslationsToReports(reports, transMap)
	return reports, nil
}

// GetReportList returns a lightweight list of reports (metadata only) for history navigation.
func GetReportList(ctx context.Context, email string) ([]Report, error) {
	rows, err := db.QueryContext(ctx, SQL.GetReportList, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []Report
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.StartDate, &r.EndDate, &r.CreatedAt); err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, nil
}

func collectReportIDs(reports []Report) []int {
	ids := make([]int, len(reports))
	for i, r := range reports {
		ids[i] = r.ID
	}
	return ids
}

func mapTranslationsToReports(reports []Report, transMap map[int]map[string]string) {
	for i := range reports {
		reports[i].Translations = transMap[reports[i].ID]
	}
}

func fetchReportMetadata(ctx context.Context, email string) ([]Report, error) {
	rows, err := db.QueryContext(ctx, SQL.ListReports, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []Report
	for rows.Next() {
		r, err := scanReportRowShort(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, nil
}

func scanReportRowShort(rows *sql.Rows) (Report, error) {
	var r Report
	var createdAt time.Time
	var isTruncated int
	var summary sql.NullString
	err := rows.Scan(&r.ID, &r.StartDate, &r.EndDate, &createdAt, &isTruncated, &summary)
	r.CreatedAt = createdAt
	r.IsTruncated = isTruncated != 0
	r.Summary = summary.String
	return r, err
}

// Why: Allows users to manage their stored reports and remove unneeded entries.
func DeleteReport(ctx context.Context, id int, email string) error {
	_, err := db.ExecContext(ctx, SQL.DeleteReport, id, email)
	return err
}

// GetMessagesForReport fetches all active messages for a user within a specified date range.
func GetMessagesForReport(ctx context.Context, email string, since time.Time) ([]ConsolidatedMessage, error) {
	sinceStr := since.Format("2006-01-02 15:04:05")

	// Why: Overriding the external query to select from v_messages, ensuring all identity-resolved columns match scanMessageRow.
	query := "SELECT * FROM v_messages WHERE user_email = ? AND (created_at >= ? OR assigned_at >= ?) AND (is_deleted = 0 OR is_deleted IS NULL) ORDER BY created_at ASC"
	rows, err := db.QueryContext(ctx, query, email, sinceStr, sinceStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]ConsolidatedMessage, error) {
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
