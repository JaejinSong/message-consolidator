-- name: RecordExtractionDecision :exec
INSERT INTO extraction_decisions (user_email, stage, verdict, source, room, source_ts, category, text_head)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);
