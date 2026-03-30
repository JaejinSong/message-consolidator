-- name: GetTaskTranslation :one
SELECT translated_text FROM task_translations WHERE message_id = ? AND language_code = ?;

-- name: GetTaskTranslationsBatch :many
SELECT message_id, translated_text FROM task_translations 
WHERE language_code = ? AND message_id IN (%s);

-- name: UpsertTaskTranslation :exec
INSERT INTO task_translations (message_id, language_code, translated_text)
VALUES (?, ?, ?)
ON CONFLICT (message_id, language_code) 
DO UPDATE SET translated_text = EXCLUDED.translated_text;
