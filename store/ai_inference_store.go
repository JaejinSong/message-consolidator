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
		MessageID:    nullInt64(int64(messageID)),
		Source:       nullString(source),
		OriginalText: nullString(originalText),
		RawResponse:  nullString(rawResponse),
	})

	if err != nil {
		logger.Errorf("[STORE] Failed to log AI inference: %v", err)
	}
	return err
}
