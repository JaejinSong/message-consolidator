-- name: CreatePromptLogsTable
CREATE TABLE IF NOT EXISTS prompt_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: InsertPromptLog
INSERT INTO prompt_logs (name, version, model, status) VALUES (?, ?, ?, ?);
