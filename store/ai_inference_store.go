package store

import (
	"database/sql"
	"message-consolidator/logger"
)

// LogAIInference records the raw AI input and output for long-term prompt performance analysis.
// Why: Enables Data Flywheel by building a dataset of real-world AI inferences for future fine-tuning and evaluation.
func LogAIInference(messageID int, source, originalText, rawResponse string) error {
	// Guard Clause: Ensure database connection is valid before execution.
	if db == nil {
		return sql.ErrConnDone
	}

	// Why: Ensures explicit integer conversion for ID parameters as required by project-specific safety rules.
	mID := int(messageID)

	_, err := db.Exec(SQL.InsertAIInferenceLog, mID, source, originalText, rawResponse)
	if err != nil {
		logger.Errorf("[STORE] Failed to log AI inference: %v", err)
	}
	return err
}
