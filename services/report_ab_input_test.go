//go:build report_ab

package services

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/store"
	"os"
	"time"
)

// abInputQuery mirrors store/queries/reports.sql :: GetMessagesForReport with the
// lower time bound dropped, so a single read serves both the activity window and the
// stalled backlog. The Go-side window/staleness filters below are the same ones
// ReportsService applies (withinWindow, fetchStalled), keeping the payload identical
// to production without running EnsureSchemaAndSeeds against the live database.
const abInputQuery = `
SELECT
    m.id, m.user_email, m.source, m.room,
    m.task,
    m.requester, m.assignee, m.assigned_at, m.link, m.source_ts, m.original_text,
    m.done, m.is_deleted, m.created_at, m.updated_at, m.completed_at, m.category,
    m.deadline, m.thread_id, m.assignee_reason, m.replied_to_id, m.is_context_query,
    m.constraints, m.metadata, m.source_channels, m.consolidated_context, m.subtasks,
    m.requester_canonical, m.assignee_canonical, m.requester_type, m.assignee_type,
    STRFTIME('%Y-%m-%dT00:00:00Z', m.deadline_date) AS deadline_date,
    COALESCE(m.deadline_inferred, 0) AS deadline_inferred
FROM v_messages m
WHERE m.user_email = ?
  AND NOT (m.done = 0 AND m.is_deleted = 1)
  AND m.category != 'merged'
ORDER BY m.created_at DESC`

// openReadOnlyDB dials the configured Turso database directly. Why: store.InitDB runs
// EnsureSchemaAndSeeds, which would apply this branch's un-deployed migrations to the
// live database; the harness only ever reads.
func openReadOnlyDB() (*sql.DB, error) {
	url := os.Getenv("TURSO_DATABASE_URL")
	if url == "" {
		return nil, fmt.Errorf("TURSO_DATABASE_URL is not set")
	}
	if token := os.Getenv("TURSO_AUTH_TOKEN"); token != "" {
		url = fmt.Sprintf("%s?authToken=%s", url, token)
	}
	return sql.Open("libsql", url)
}

// fetchABLogs reads every report-eligible task once and splits it into the activity
// window and the stalled backlog using the production predicates.
func fetchABLogs(ctx context.Context, db *sql.DB, email, startDate, endDate string) (activity, stalled []Log, err error) {
	rows, err := db.QueryContext(ctx, abInputQuery, email)
	if err != nil {
		return nil, nil, fmt.Errorf("query v_messages: %w", err)
	}
	defer rows.Close()

	threshold := store.GetStaleThresholdWorkingDays()
	now := time.Now()
	var all []Log
	for rows.Next() {
		m, scanErr := scanABLog(rows)
		if scanErr != nil {
			return nil, nil, scanErr
		}
		all = append(all, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	activity = withinWindow(all, startDate, endDate)
	for _, m := range all {
		if m.Done {
			continue
		}
		if ds := m.CreatedAt.Format("2006-01-02"); ds >= startDate {
			continue
		}
		base := m.CreatedAt
		if !m.AssignedAt.IsZero() && m.AssignedAt.After(base) {
			base = m.AssignedAt
		}
		if store.WorkingDaysSince(base, now) >= threshold {
			stalled = append(stalled, m)
		}
	}
	return activity, stalled, nil
}

func scanABLog(rows *sql.Rows) (Log, error) {
	var (
		id                                                       int64
		userEmail, source, room, task, requester, assignee, link string
		sourceTs, originalText, category, deadline, threadID     string
		asgReason, repliedToID, constraints, metadata, channels  string
		contextStr, subtasks, reqCanonical, asgCanonical         string
		reqType, asgType                                         string
		done, isDeleted                                          bool
		isContextQuery, deadlineInferred                         int64
		assignedAt, createdAt, updatedAt, completedAt            sql.NullTime
		deadlineDate                                             sql.NullString
	)
	if err := rows.Scan(
		&id, &userEmail, &source, &room, &task,
		&requester, &assignee, &assignedAt, &link, &sourceTs, &originalText,
		&done, &isDeleted, &createdAt, &updatedAt, &completedAt, &category,
		&deadline, &threadID, &asgReason, &repliedToID, &isContextQuery,
		&constraints, &metadata, &channels, &contextStr, &subtasks,
		&reqCanonical, &asgCanonical, &reqType, &asgType,
		&deadlineDate, &deadlineInferred,
	); err != nil {
		return Log{}, fmt.Errorf("scan v_messages row: %w", err)
	}
	return store.MapVMessageToConsolidated(
		store.MessageID(id), userEmail, source, room, task, requester, assignee, link, sourceTs,
		originalText, done, isDeleted, createdAt,
		category, deadline, threadID, reqCanonical, asgCanonical, asgReason,
		repliedToID, int(isContextQuery), constraints, contextStr,
		metadata, channels, reqType, asgType, subtasks,
		assignedAt, completedAt, updatedAt,
		store.NullTimeFromInterface(nullStringValue(deadlineDate)), deadlineInferred,
	), nil
}

// nullStringValue unwraps a NULL-able DATE column for store.NullTimeFromInterface,
// which parses the RFC3339 string the query's STRFTIME produces.
func nullStringValue(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}
