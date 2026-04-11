package store

import (
	"database/sql"
	"encoding/json"
)

// scanMessageRow maps a database row to the ConsolidatedMessage struct.
// Why: Enforces the 30-line limit and resolves a variable declaration bug by delegating field assignment to populateFields.
func scanMessageRow(rows interface{ Scan(...interface{}) error }) (ConsolidatedMessage, error) {
	var m ConsolidatedMessage
	var assignedAt, createdAt, completedAt DBTime
	var constraints, room, requester, assignee, link, originalText, category, deadline, threadID, assigneeReason, repliedToID, sourceTS, source, reqCanonical, asgCanonical, metadata, sourceChannels, consolidatedContext, reqType, asgType sql.NullString

	err := rows.Scan(
		&m.ID, &m.UserEmail, &source, &room, &m.Task,
		&requester, &assignee, &assignedAt, &link,
		&sourceTS, &originalText, &m.Done, &m.IsDeleted,
		&createdAt, &completedAt, &category, &deadline,
		&threadID, &assigneeReason, &repliedToID,
		&m.IsContextQuery, &constraints, &metadata, &sourceChannels,
		&consolidatedContext,
		&reqCanonical, &asgCanonical,
		&reqType, &asgType,
	)
	if err != nil {
		return m, err
	}

	populateFields(&m, source, room, requester, assignee, link, sourceTS, originalText, category, deadline, threadID, assigneeReason, repliedToID, reqCanonical, asgCanonical, reqType, asgType)
	parseMetadata(&m, constraints, metadata, sourceChannels, consolidatedContext, assignedAt, createdAt, completedAt)
	return m, nil
}

func populateFields(
	m *ConsolidatedMessage,
	source, room, req, asg, link, ts, text, cat, dl, tid, reason, reply, reqC, asgC, reqT, asgT sql.NullString,
) {
	m.Source = source.String
	m.Room = room.String
	m.Requester = req.String
	m.Assignee = asg.String
	m.Link = link.String
	m.SourceTS = ts.String
	m.OriginalText = text.String
	m.Category = cat.String
	m.Deadline = dl.String
	m.ThreadID = tid.String
	m.AssigneeReason = reason.String
	m.RepliedToID = reply.String
	m.RequesterCanonical = reqC.String
	m.AssigneeCanonical = asgC.String
	m.RequesterType = reqT.String
	m.AssigneeType = asgT.String
}

func parseMetadata(m *ConsolidatedMessage, constraints, metadata, sourceChannels, consolidatedContext sql.NullString, assignedAt, createdAt, completedAt DBTime) {
	// Why: Unmarshals JSON strings into segments while providing safe defaults for nil constraints.
	if constraints.Valid && constraints.String != "" {
		_ = json.Unmarshal([]byte(constraints.String), &m.Constraints)
	}
	if metadata.Valid && metadata.String != "" {
		_ = json.Unmarshal([]byte(metadata.String), &m.Metadata)
	}
	if sourceChannels.Valid && sourceChannels.String != "" {
		_ = json.Unmarshal([]byte(sourceChannels.String), &m.SourceChannels)
	}
	if consolidatedContext.Valid && consolidatedContext.String != "" {
		_ = json.Unmarshal([]byte(consolidatedContext.String), &m.ConsolidatedContext)
	}
	if m.SourceChannels == nil {
		m.SourceChannels = []string{m.Source}
	}
	if m.Constraints == nil {
		m.Constraints = []string{}
	}
	if m.ConsolidatedContext == nil {
		m.ConsolidatedContext = []string{}
	}

	m.AssignedAt = assignedAt.Time
	m.CreatedAt = createdAt.Time
	if completedAt.Valid && !completedAt.Time.IsZero() {
		m.CompletedAt = &completedAt.Time
	}
	if m.AssignedAt.IsZero() && !m.CreatedAt.IsZero() {
		m.AssignedAt = m.CreatedAt
	}
}
