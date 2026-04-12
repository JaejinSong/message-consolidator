package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"strings"
	"time"
)

func GetReport(ctx context.Context, email, start, end string) (*Report, error) {
	row, err := db.New(GetDB()).GetReport(ctx, db.GetReportParams{
		UserEmail: email, StartDate: start, EndDate: end,
	})
	if err != nil {
		return nil, err
	}
	r := reportFromRow(int(row.ID), row.UserEmail, row.StartDate, row.EndDate,
		row.Visualization, row.IsTruncated.Int64, row.CreatedAt.Time, row.Summary)
	r.Translations, _ = GetReportTranslations(ctx, r.ID)
	return r, nil
}

// GetReportByDate retrieves a report for a specific date (YYYY-MM-DD).
// Why: Enables exact date-based caching to avoid redundant AI generation for the same day.
func GetReportByDate(ctx context.Context, email, date string) (*Report, error) {
	row, err := db.New(GetDB()).GetReportByDate(ctx, db.GetReportByDateParams{
		UserEmail: email, StartDate: date, EndDate: date,
	})
	if err != nil {
		return nil, err
	}
	r := reportFromRow(int(row.ID), row.UserEmail, row.StartDate, row.EndDate,
		row.Visualization, row.IsTruncated.Int64, row.CreatedAt.Time, row.Summary)
	r.Translations, _ = GetReportTranslations(ctx, r.ID)
	return r, nil
}

// Why: Retrieves a specific report by its unique ID, ensuring it belongs to the requesting user.
func GetReportByID(ctx context.Context, id int, email string) (*Report, error) {
	row, err := db.New(GetDB()).GetReportByID(ctx, db.GetReportByIDParams{ID: int64(id), UserEmail: email})
	if err != nil {
		return nil, err
	}
	r := reportFromRow(int(row.ID), row.UserEmail, row.StartDate, row.EndDate,
		row.Visualization, row.IsTruncated.Int64, row.CreatedAt.Time, row.Summary)
	r.Translations, _ = GetReportTranslations(ctx, r.ID)
	return r, nil
}

// Why: Saves the metadata portion of a report and returns the generated primary key.
// Uses sqlc-generated InsertReport to avoid SQL.InsertReport being an empty bridge string.
func SaveReport(ctx context.Context, r *Report) (int64, error) {
	isTruncated := 0
	if r.IsTruncated {
		isTruncated = 1
	}
	newID, err := db.New(GetDB()).InsertReport(ctx, db.InsertReportParams{
		UserEmail:     r.UserEmail,
		StartDate:     r.StartDate,
		EndDate:       r.EndDate,
		Visualization: r.Visualization,
		IsTruncated:   sql.NullInt64{Int64: int64(isTruncated), Valid: true},
	})
	if err != nil {
		return 0, err
	}
	return int64(newID), nil
}

// Why: Persists a specific language translation for a given report metadata entry.
func SaveReportTranslation(ctx context.Context, reportID int64, langCode, summary string) error {
	return db.New(GetDB()).InsertReportTranslation(ctx, db.InsertReportTranslationParams{
		ReportID:     reportID,
		LanguageCode: langCode,
		Summary:      summary,
	})
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
	conn := GetDB()
	rows, err := conn.QueryContext(ctx, query, args...)
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
	rows, err := db.New(GetDB()).GetReportTranslations(ctx, int64(reportID))
	if err != nil {
		return nil, err
	}
	translations := make(map[string]string)
	for _, row := range rows {
		translations[row.LanguageCode] = row.Summary
	}
	return translations, nil
}

// reportFromRow maps sqlc row fields to a Report domain object.
func reportFromRow(id int, email, start, end, viz string, isTruncated int64, createdAt time.Time, summary string) *Report {
	return &Report{
		ID: id, UserEmail: email, StartDate: start, EndDate: end,
		Visualization: viz, IsTruncated: isTruncated != 0,
		CreatedAt: createdAt, Summary: summary,
		Translations: make(map[string]string),
	}
}

// Why: Provides a chronological list of a user's generated reports for the UI sidebar.
// Refactored: Uses 1+1 pattern (metadata batch + translations batch) to eliminate N+1 overhead.
func ListReports(ctx context.Context, email string) ([]Report, error) {
	rows, err := db.New(GetDB()).ListReports(ctx, email)
	if err != nil {
		return nil, err
	}
	var reports []Report
	for _, row := range rows {
		r := reportFromRow(int(row.ID), email, row.StartDate, row.EndDate,
			"", row.IsTruncated.Int64, row.CreatedAt.Time, row.Summary)
		reports = append(reports, *r)
	}
	if len(reports) == 0 {
		return reports, nil
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
	conn := GetDB()
	rows, err := conn.QueryContext(ctx, SQL.GetReportList, email)
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

func mapReportRowToReport(row db.GetReportByIDRow) *Report {
	var r Report
	r.ID = int(row.ID)
	r.StartDate = row.StartDate
	r.EndDate = row.EndDate
	r.CreatedAt = row.CreatedAt.Time
	r.IsTruncated = row.IsTruncated.Int64 != 0
	r.Summary = row.Summary
	r.Visualization = row.Visualization
	return &r
}

func mapReportByDateRowToReport(row db.GetReportByDateRow) *Report {
	var r Report
	r.ID = int(row.ID)
	r.StartDate = row.StartDate
	r.EndDate = row.EndDate
	r.CreatedAt = row.CreatedAt.Time
	r.IsTruncated = row.IsTruncated.Int64 != 0
	r.Summary = row.Summary
	r.Visualization = row.Visualization
	return &r
}

// Why: Allows users to manage their stored reports and remove unneeded entries.
func DeleteReport(ctx context.Context, id int, email string) error {
	return db.New(GetDB()).DeleteReport(ctx, db.DeleteReportParams{
		ID: int64(id), UserEmail: email,
	})
}

// GetMessagesForReport fetches all active messages for a user within a specified date range.
func GetMessagesForReport(ctx context.Context, email string, since time.Time) ([]ConsolidatedMessage, error) {
	sinceStr := since.Format("2006-01-02 15:04:05")

	// Why: Overriding the external query to select from v_messages, ensuring all identity-resolved columns match scanMessageRow.
	query := "SELECT * FROM v_messages WHERE user_email = ? AND (created_at >= ? OR assigned_at >= ?) AND (is_deleted = 0 OR is_deleted IS NULL) ORDER BY created_at ASC"
	conn := GetDB()
	rows, err := conn.QueryContext(ctx, query, email, sinceStr, sinceStr)
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
