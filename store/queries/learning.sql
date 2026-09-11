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

-- name: PrecisionBucketsByOwner :many
-- Cancel rate per (source, room, owner-class) over resolved tasks. Why: the user's own
-- triage is the only ground truth, and ownership is the strongest signal measured --
-- every head verb cancels 7-46 points worse when the task is unowned (2026-09-10).
-- Active tasks are excluded: they carry no decision yet.
SELECT COALESCE(source, '') AS source,
       COALESCE(room, '') AS room,
       CASE WHEN assignee = 'shared' THEN 'shared' ELSE 'named' END AS owner_class,
       COUNT(*) AS resolved,
       SUM(CASE WHEN done = 0 AND is_deleted = 1 THEN 1 ELSE 0 END) AS canceled,
       COALESCE(group_concat(CASE WHEN done = 0 AND is_deleted = 1 THEN id END), '') AS canceled_ids
FROM messages
WHERE user_email = ?
  AND IFNULL(task, '') <> ''
  AND created_at >= ?
  AND (done = 1 OR is_deleted = 1)
GROUP BY source, room, owner_class;

-- name: PrecisionBucketsByVerb :many
-- Same, keyed on the title's leading verb. Why: it spans 15.8% to 83.3% cancel, so it
-- carries real signal -- generic verbs the extractor falls back to when the message named
-- no specific action (review, update, check) sit at the bad end. Grouped by source only:
-- per-room verb buckets are too thin to clear the volume floor.
SELECT COALESCE(source, '') AS source,
       lower(substr(task, 1, instr(task || ' ', ' ') - 1)) AS verb,
       COUNT(*) AS resolved,
       SUM(CASE WHEN done = 0 AND is_deleted = 1 THEN 1 ELSE 0 END) AS canceled,
       COALESCE(group_concat(CASE WHEN done = 0 AND is_deleted = 1 THEN id END), '') AS canceled_ids
FROM messages
WHERE user_email = ?
  AND IFNULL(task, '') <> ''
  AND created_at >= ?
  AND (done = 1 OR is_deleted = 1)
GROUP BY source, verb;

-- name: UpsertPrecisionObservation :exec
-- Idempotent per bucket: from_value/to_value/scope are stable so the UNIQUE key holds
-- across runs, and only the measured counts and the sample ids move. to_value stays empty
-- on purpose -- putting the changing statistic there would make every measurement a new row.
INSERT INTO correction_observations
    (user_email, kind, from_value, to_value, scope, evidence_count, seen_message_ids, status)
VALUES (?, 'precision', ?, '', ?, ?, ?, 'pending')
ON CONFLICT(user_email, kind, from_value, to_value, scope) DO UPDATE SET
    evidence_count = excluded.evidence_count,
    seen_message_ids = excluded.seen_message_ids,
    updated_at = CURRENT_TIMESTAMP
WHERE correction_observations.status <> 'rejected';

-- name: ListPrecisionObservations :many
-- Why: kind='precision' is deliberately outside ListActiveSuppressRules' filter, so these
-- can never be applied by guardSuppressRule -- they exist to be read and approved.
SELECT id, from_value, scope, evidence_count, seen_message_ids, status, updated_at
FROM correction_observations
WHERE user_email = ? AND kind = 'precision'
ORDER BY evidence_count DESC, updated_at DESC;
