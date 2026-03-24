-- name: GetTaskTranslation :one
SELECT translated_text FROM task_translations WHERE message_id = ? AND language = ?;

-- name: GetTaskTranslationsBatch :many
SELECT message_id, translated_text FROM task_translations 
WHERE language = ? AND message_id IN (%s);

-- name: UpsertTaskTranslation :exec
INSERT INTO task_translations (message_id, language, translated_text)
VALUES (?, ?, ?)
ON CONFLICT (message_id, language) 
DO UPDATE SET translated_text = EXCLUDED.translated_text;
