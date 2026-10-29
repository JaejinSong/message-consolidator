-- name: GetTaskTranslationsBatch :many
SELECT tt.message_id, tt.translated_text, m.task FROM task_translations tt
JOIN messages m ON m.id = tt.message_id
WHERE tt.language_code = ? AND tt.message_id IN (sqlc.slice('message_ids'));

-- name: SaveTaskTranslationForSource :one
INSERT INTO task_translations (message_id, language_code, translated_text)
SELECT m.id, sqlc.arg(language_code), sqlc.arg(translated_text) FROM messages m
WHERE m.id = sqlc.arg(message_id) AND m.task = sqlc.arg(source_task)
ON CONFLICT (message_id, language_code)
DO UPDATE SET translated_text = EXCLUDED.translated_text
RETURNING message_id;

-- name: DeleteTaskTranslations :exec
DELETE FROM task_translations WHERE message_id = ?;
