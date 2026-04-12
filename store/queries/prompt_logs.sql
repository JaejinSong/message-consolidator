-- name: InsertPromptLog :exec
INSERT INTO prompt_logs (name, version, model, status) VALUES (?, ?, ?, ?);
