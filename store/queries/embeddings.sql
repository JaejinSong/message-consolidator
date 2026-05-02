-- name: UpsertMessageEmbedding :exec
INSERT INTO message_embeddings (message_id, model, dim, vec, text_hash)
VALUES (?1, ?2, ?3, ?4, ?5)
ON CONFLICT(message_id) DO UPDATE SET
  model = excluded.model,
  dim = excluded.dim,
  vec = excluded.vec,
  text_hash = excluded.text_hash,
  created_at = CURRENT_TIMESTAMP;

-- name: GetMessageEmbedding :one
SELECT message_id, model, dim, vec, text_hash, created_at
FROM message_embeddings
WHERE message_id = ?1;

-- name: DeleteEmbeddingsByModel :exec
DELETE FROM message_embeddings WHERE model = ?1;

-- name: ListMissingEmbeddingsForUser :many
SELECT m.id, COALESCE(m.task, '') AS task, COALESCE(m.original_text, '') AS original_text
FROM messages m
LEFT JOIN message_embeddings e ON e.message_id = m.id
WHERE m.lifecycle != 'active'
  AND m.user_email = ?1
  AND IFNULL(m.task, '') != ''
  AND (e.message_id IS NULL OR e.model != ?2)
ORDER BY m.completed_at DESC, m.id DESC
LIMIT ?3;

-- name: CountMissingEmbeddingsForUser :one
SELECT COUNT(*)
FROM messages m
LEFT JOIN message_embeddings e ON e.message_id = m.id
WHERE m.lifecycle != 'active'
  AND m.user_email = ?1
  AND IFNULL(m.task, '') != ''
  AND (e.message_id IS NULL OR e.model != ?2);

-- name: ListArchiveEmbeddingsPage :many
SELECT e.message_id, e.vec
FROM message_embeddings e
JOIN messages m ON m.id = e.message_id
WHERE m.lifecycle != 'active'
  AND m.user_email = ?1
  AND e.model = ?2
ORDER BY e.message_id
LIMIT ?3 OFFSET ?4;
