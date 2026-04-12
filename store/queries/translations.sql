-- name: GetTaskTranslation :one
SELECT translated_text FROM task_translations WHERE message_id = ? AND language_code = ?;

-- name: GetTaskTranslationsBatch :many
SELECT message_id, translated_text FROM task_translations 
WHERE language_code = ? AND message_id IN (sqlc.slice('message_ids'));

-- name: UpsertTaskTranslation :exec
INSERT INTO task_translations (message_id, language_code, translated_text)
VALUES (?, ?, ?)
ON CONFLICT (message_id, language_code) 
DO UPDATE SET translated_text = EXCLUDED.translated_text;

-- name: MigrateTaskTranslationsRenameLanguage :exec
ALTER TABLE task_translations RENAME COLUMN language TO language_deprecated;

-- name: MigrateTaskTranslationsAddLanguageCode :exec
ALTER TABLE task_translations ADD COLUMN language_code TEXT;

-- name: CreateIdxTaskTranslationsIDLangCode :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_trans_id_lang ON task_translations(message_id, language_code);

-- name: DeleteTaskTranslations :exec
DELETE FROM task_translations WHERE message_id = ?;
