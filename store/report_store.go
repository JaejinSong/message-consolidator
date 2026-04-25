package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
	"time"
)

// GetReportByDateRange retrieves a report matching both start and end dates exactly.
func GetReportByDateRange(ctx context.Context, email, start, end string) (*Report, error) {
	row, err := db.New(GetDB()).GetReportByDate(ctx, db.GetReportByDateParams{
		UserEmail: email, StartDate: start, EndDate: end,
	})
	if err != nil {
		return nil, err
	}
	r := reportFromRow(int(row.ID), row.UserEmail, row.StartDate, row.EndDate,
		row.Visualization, row.Status.String, row.IsTruncated.Int64, row.CreatedAt.Time, row.Summary)
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
		row.Visualization, row.Status.String, row.IsTruncated.Int64, row.CreatedAt.Time, row.Summary)
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
	if r.Status == "" {
		r.Status = "completed"
	}
	newID, err := db.New(GetDB()).InsertReport(ctx, db.InsertReportParams{
		UserEmail:     r.UserEmail,
		StartDate:     r.StartDate,
		EndDate:       r.EndDate,
		Visualization: r.Visualization,
		Status:        nullString(r.Status),
		IsTruncated:   nullInt64(int64(isTruncated)),
	})
	if err != nil {
		return 0, err
	}
	return int64(newID), nil
}

// UpdateReportStatus updates the generation status and visualization data for a report.
func UpdateReportStatus(ctx context.Context, status, viz string, isTruncated bool, id int, email string) error {
	truncVal := int64(0)
	if isTruncated {
		truncVal = 1
	}
	return db.New(GetDB()).UpdateReportStatus(ctx, db.UpdateReportStatusParams{
		Status:        nullString(status),
		Visualization: viz,
		IsTruncated:   nullInt64(truncVal),
		ID:            int64(id),
		UserEmail:     email,
	})
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

	ids := make([]int64, len(reportIDs))
	for i, id := range reportIDs {
		ids[i] = int64(id)
	}
	rows, err := db.New(GetDB()).GetReportTranslationsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	res := make(map[int]map[string]string)
	for _, row := range rows {
		rid := int(row.ReportID)
		if res[rid] == nil {
			res[rid] = make(map[string]string)
		}
		res[rid][row.LanguageCode] = row.Summary
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
func reportFromRow(id int, email, start, end, viz, status string, isTruncated int64, createdAt time.Time, summary string) *Report {
	if status == "" {
		status = "completed"
	}
	return &Report{
		ID: id, UserEmail: email, StartDate: start, EndDate: end,
		Visualization: viz, Status: status, IsTruncated: isTruncated != 0,
		CreatedAt: createdAt, ReportSummary: summary,
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
			"", row.Status.String, row.IsTruncated.Int64, row.CreatedAt.Time, row.Summary)
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
	rows, err := db.New(GetDB()).GetReportList(ctx, email)
	if err != nil {
		return nil, err
	}

	var reports []Report
	for _, row := range rows {
		reports = append(reports, Report{
			ID:        int(row.ID),
			StartDate: row.StartDate,
			EndDate:   row.EndDate,
			CreatedAt: row.CreatedAt.Time,
			Status:    row.Status.String,
		})
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


// Why: Allows users to manage their stored reports and remove unneeded entries.
func DeleteReport(ctx context.Context, id int, email string) error {
	return db.New(GetDB()).DeleteReport(ctx, db.DeleteReportParams{
		ID: int64(id), UserEmail: email,
	})
}

// GetMessagesForReport fetches active messages for a user with optional source and status filters.
func GetMessagesForReport(ctx context.Context, email string, since time.Time, source *string, done *bool) ([]ConsolidatedMessage, error) {
	arg := db.GetMessagesForReportParams{
		UserEmail:  email,
		CreatedAt:  sql.NullTime{Time: since, Valid: true},
		AssignedAt: sql.NullTime{Time: since, Valid: true},
	}
	if source != nil {
		arg.Source = *source
	}
	if done != nil {
		arg.Done = *done
	}

	rows, err := db.New(GetDB()).GetMessagesForReport(ctx, arg)
	if err != nil {
		return nil, LogSQLError("GetMessagesForReportFiltered", err, email, since)
	}

	msgs := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		msgs[i] = toConsolidatedFromByMessages(row)
	}
	return msgs, nil
}

func toConsolidatedFromByMessages(row db.VMessage) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		int(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt,
	)
}
