package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
	"message-consolidator/logger"
)

// LogAIInference records the raw AI input and output for long-term prompt performance analysis.
// Why: Enables Data Flywheel by building a dataset of real-world AI inferences for future fine-tuning and evaluation.
func LogAIInference(messageID int, source, originalText, rawResponse string) error {
	conn := GetDB()
	if conn == nil {
		return sql.ErrConnDone
	}

	queries := db.New(conn)
	// Why: Parameters are handled via sql.Null wrappers because the underlying table columns are nullable.
	err := queries.InsertAIInferenceLog(context.Background(), db.InsertAIInferenceLogParams{
		MessageID:    sql.NullInt64{Int64: int64(messageID), Valid: true},
		Source:       sql.NullString{String: source, Valid: true},
		OriginalText: sql.NullString{String: originalText, Valid: true},
		RawResponse:  sql.NullString{String: rawResponse, Valid: true},
	})

	if err != nil {
		logger.Errorf("[STORE] Failed to log AI inference: %v", err)
	}
	return err
}
