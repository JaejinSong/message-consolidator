package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
	"message-consolidator/logger"
)

// LogAIInference records AI call metadata (message_id, source) for volume tracking.
// Why: payload (original_text, raw_response) is written to /app/logs/ai_inference.log by
// logger.LogAIInferenceToFile before this call — storing it again in DB was pure Bytes Synced waste.
func LogAIInference(messageID MessageID, source string) error {
	conn := GetDB()
	if conn == nil {
		return sql.ErrConnDone
	}

	queries := db.New(conn)
	err := queries.InsertAIInferenceLog(context.Background(), db.InsertAIInferenceLogParams{
		MessageID: nullInt64(int64(messageID)),
		Source:    nullString(source),
	})

	if err != nil {
		logger.Errorf("[STORE] Failed to log AI inference: %v", err)
	}
	return err
}
