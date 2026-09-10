-- name: RecordExtractionDecision :exec
INSERT INTO extraction_decisions (user_email, stage, verdict, source, room, source_ts, category, text_head)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListExtractionDecisions :many
-- Newest first, for sampling what the pipeline threw away.
SELECT id, user_email, stage, verdict, source, room, source_ts, category, text_head, created_at
FROM extraction_decisions
WHERE user_email = ?
  AND (sqlc.narg('stage') IS NULL OR stage = sqlc.narg('stage'))
  AND created_at >= ?
ORDER BY created_at DESC
LIMIT ?;

-- name: CountExtractionDecisions :many
-- Drop counts per stage/verdict/source, to compare against task creation over the same window.
SELECT stage, verdict, source, COUNT(*) AS total
FROM extraction_decisions
WHERE user_email = ? AND created_at >= ?
GROUP BY stage, verdict, source
ORDER BY total DESC;
