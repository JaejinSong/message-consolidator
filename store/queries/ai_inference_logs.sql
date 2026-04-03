-- name: CreateAIInferenceLogsTable :exec
CREATE TABLE IF NOT EXISTS ai_inference_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER,
    source TEXT,
    original_text TEXT,
    raw_response TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: InsertAIInferenceLog :exec
INSERT INTO ai_inference_logs (message_id, source, original_text, raw_response)
VALUES (?, ?, ?, ?);
