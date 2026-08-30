-- name: InsertLearnedExample :exec
INSERT INTO learned_examples (user_email, source, lang, input, expected, origin, message_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_email, message_id, origin) DO NOTHING;

-- name: ListLearnedExamples :many
SELECT id, user_email, source, lang, input, expected, origin, message_id, created_at
FROM learned_examples
WHERE user_email = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ListLearnedExamplesBySource :many
SELECT id, user_email, source, lang, input, expected, origin, message_id, created_at
FROM learned_examples
WHERE user_email = ? AND source = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: CountLearnedExamplesByOrigin :one
SELECT COUNT(*) FROM learned_examples
WHERE user_email = ? AND origin = ?;

-- name: DeleteLearnedExample :exec
DELETE FROM learned_examples WHERE id = ? AND user_email = ?;

-- name: GetCorrectionObservation :one
SELECT id, user_email, kind, from_value, to_value, scope, evidence_count, seen_message_ids, status, created_at, updated_at
FROM correction_observations
WHERE user_email = ? AND kind = ? AND from_value = ? AND to_value = ? AND scope = ?;

-- name: InsertCorrectionObservation :exec
INSERT INTO correction_observations (user_email, kind, from_value, to_value, scope, evidence_count, seen_message_ids, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateCorrectionObservationEvidence :exec
UPDATE correction_observations
SET evidence_count = ?, seen_message_ids = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateCorrectionObservationStatus :exec
UPDATE correction_observations
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND user_email = ?;

-- name: ListCorrectionObservationsByStatus :many
SELECT id, user_email, kind, from_value, to_value, scope, evidence_count, seen_message_ids, status, created_at, updated_at
FROM correction_observations
WHERE user_email = ? AND status = ?
ORDER BY updated_at DESC;

-- name: ListActiveSuppressRules :many
SELECT id, user_email, kind, from_value, to_value, scope, evidence_count, seen_message_ids, status, created_at, updated_at
FROM correction_observations
WHERE user_email = ? AND kind = 'suppress' AND status IN ('promoted', 'approved');
