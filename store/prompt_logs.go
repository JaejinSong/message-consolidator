package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
)

// LogPromptExecution records the metadata and status of an AI prompt execution in SQLite.
// Why: Enables long-term auditing and performance tracking of specialized AI prompts (v1, v2, etc.) independently of application logs.
func LogPromptExecution(dbConn *sql.DB, name, version, model, status string) error {
	// Guard Clause: Ensure database connection is valid before execution.
	if dbConn == nil {
		return sql.ErrConnDone
	}

	return db.New(dbConn).InsertPromptLog(context.Background(), db.InsertPromptLogParams{
		Name:    name,
		Version: version,
		Model:   model,
		Status:  status,
	})
}
