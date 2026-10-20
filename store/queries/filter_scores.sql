-- name: InsertFilterScore :one
INSERT INTO filter_scores (user_email, source, room, source_ts, message_ref, provider, p_actionable, threshold, verdict, category, text)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetFilterScore :one
SELECT id, user_email, source, room, source_ts, message_ref, provider, p_actionable, threshold, verdict, category, text, outcome, outcome_at, created_at
FROM filter_scores
WHERE id = ? AND user_email = ?;

-- name: ListFilterDrops :many
-- Newest first, noise verdicts only.
SELECT id, user_email, source, room, source_ts, message_ref, provider, p_actionable, threshold, verdict, category, text, outcome, outcome_at, created_at
FROM filter_scores
WHERE user_email = ? AND verdict = 'noise' AND created_at >= ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListBorderlineDrops :many
-- Unlabeled noise drops whose score sits in [lo, hi), closest to the threshold first.
SELECT id, user_email, source, room, source_ts, message_ref, provider, p_actionable, threshold, verdict, category, text, outcome, outcome_at, created_at
FROM filter_scores
WHERE user_email = sqlc.arg(user_email) AND verdict = 'noise' AND outcome = ''
  AND created_at >= sqlc.arg(since)
  AND p_actionable >= sqlc.arg(lo) AND p_actionable < sqlc.arg(hi)
ORDER BY p_actionable DESC
LIMIT sqlc.arg(row_limit);

-- name: SetFilterOutcome :exec
UPDATE filter_scores
SET outcome = ?, outcome_at = CURRENT_TIMESTAMP
WHERE id = ? AND user_email = ?;

-- name: LinkFilterOutcomeBySourceTS :exec
-- Only unlabeled pass rows are linked, so a later link never overwrites an earlier label.
UPDATE filter_scores
SET outcome = ?, outcome_at = CURRENT_TIMESTAMP
WHERE user_email = ? AND source = ? AND room = ? AND source_ts = ?
  AND verdict = 'pass' AND outcome = '';

-- name: ListFilterScoresSince :many
SELECT id, user_email, source, room, source_ts, message_ref, provider, p_actionable, threshold, verdict, category, text, outcome, outcome_at, created_at
FROM filter_scores
WHERE user_email = ? AND created_at >= ?
ORDER BY created_at ASC, id ASC;

-- name: ListFilterUsers :many
SELECT DISTINCT user_email
FROM filter_scores
WHERE created_at >= ?;

-- name: DeleteFilterScoresBefore :exec
DELETE FROM filter_scores WHERE created_at < ?;

-- name: LabelDeletedFilterScore :exec
-- A deleted undone task marks its passed verdict a false pass, overriding the earlier 'task' link.
-- Chat rows key on the latest message id, so any id in message_ref also matches.
UPDATE filter_scores
SET outcome = 'deleted_noise', outcome_at = CURRENT_TIMESTAMP
WHERE user_email = sqlc.arg(user_email) AND source = sqlc.arg(source) AND room = sqlc.arg(room)
  AND verdict = 'pass' AND outcome IN ('', 'task')
  AND (source_ts = sqlc.arg(source_ts)
       OR instr(',' || message_ref || ',', ',' || sqlc.arg(source_ts) || ',') > 0);

-- name: ClaimFilterRescue :execrows
-- Compare-and-swap claim: only a rescuer holding the current outcome sees rows affected = 1.
UPDATE filter_scores
SET outcome = 'rescued', outcome_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND user_email = sqlc.arg(user_email) AND verdict = 'noise'
  AND outcome <> 'rescued' AND outcome = sqlc.arg(prev_outcome);

-- name: RestoreFilterOutcome :exec
-- Reverts a claim whose extraction failed so the drop stays retryable.
UPDATE filter_scores
SET outcome = ?, outcome_at = CURRENT_TIMESTAMP
WHERE id = ? AND user_email = ? AND outcome = 'rescued';

-- name: ConfirmFilterNoise :execrows
-- Rescued rows are left untouched.
UPDATE filter_scores
SET outcome = 'confirmed_noise', outcome_at = CURRENT_TIMESTAMP
WHERE id = ? AND user_email = ? AND verdict = 'noise' AND outcome <> 'rescued';
