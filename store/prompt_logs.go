package store

import (
	"database/sql"
)

// LogPromptExecution records the metadata and status of an AI prompt execution in SQLite.
// Why: Enables long-term auditing and performance tracking of specialized AI prompts (v1, v2, etc.) independently of application logs.
func LogPromptExecution(db *sql.DB, name, version, model, status string) error {
	// Guard Clause: Ensure database connection is valid before execution.
	if db == nil {
		return sql.ErrConnDone
	}

	_, err := db.Exec(SQL.InsertPromptLog, name, version, model, status)
	return err
}
