-- name: GetTotalCompleted :one
SELECT COUNT(*) FROM v_messages WHERE user_email = CAST(?1 AS TEXT) AND done = 1;

-- name: GetPendingMe :one
SELECT COUNT(*) FROM v_messages 
WHERE user_email = CAST(?1 AS TEXT) AND done = 0 AND is_deleted = 0 
AND (assignee = CAST(?2 AS TEXT) OR assignee = 'me') 
AND IFNULL(task, '') != '';



-- name: GetDailyCompletions :many
SELECT strftime('%Y-%m-%d', completed_at, ?) as d, COUNT(*) as c
FROM messages 
WHERE user_email = ? AND done = 1 
AND completed_at > datetime('now', ?, '-30 days')
GROUP BY 1 ORDER BY 1;

-- name: GetHourlyActivity :many
SELECT strftime('%H', completed_at, ?) as hr, COUNT(*) as c
FROM messages 
WHERE user_email = ? AND done = 1 AND completed_at IS NOT NULL
GROUP BY 1 ORDER BY 1;

-- name: GetAbandonedTasks :one
SELECT COUNT(*) FROM v_messages 
WHERE user_email = ? AND done = 0 AND is_deleted = 0 
AND created_at < ? AND (assignee != ? AND assignee != 'me')
AND IFNULL(task, '') != '';

-- name: GetSourceDistributionActive :many
SELECT source, COUNT(*) FROM v_messages 
WHERE user_email = ? AND is_deleted = 0 AND IFNULL(task, '') != ''
GROUP BY source;

-- name: GetSourceDistributionTotal :many
SELECT source, COUNT(*) FROM v_messages 
WHERE user_email = ? AND IFNULL(task, '') != ''
GROUP BY source;

-- name: GetCompletionHistory :many
SELECT strftime('%Y-%m-%d', completed_at, ?) as c_date, source, COUNT(*) 
FROM messages 
WHERE user_email = ? AND done = 1 
AND completed_at >= datetime('now', ?, '-365 days')
GROUP BY 1, 2 ORDER BY 1 ASC;

-- name: GetEarlyBirdCompleted :one
SELECT COUNT(*) FROM messages
WHERE user_email = CAST(?1 AS TEXT) AND done = 1
AND strftime('%H', completed_at, 'localtime') < '09';

-- name: GetMaxDailyCompleted :one
SELECT COALESCE(MAX(c), 0) FROM (
  SELECT COUNT(*) as c FROM messages 
  WHERE user_email = CAST(?1 AS TEXT) AND done = 1
  GROUP BY strftime('%Y-%m-%d', completed_at, 'localtime')
);

-- name: GetEmergencyCompleted :one
SELECT COUNT(*) FROM messages
WHERE user_email = ? AND done = 1
AND category = 'emergency';

-- name: GetPendingOthers :one
SELECT COUNT(*) FROM v_messages 
WHERE user_email = ? AND done = 0 AND is_deleted = 0 
AND (assignee != ? AND assignee != 'me') 
AND IFNULL(task, '') != '';

-- name: GetTaskCountByContactType :many
SELECT requester_type AS contact_type, COUNT(*) AS count
FROM v_messages 
WHERE user_email = ? AND is_deleted = 0 
GROUP BY requester_type;

-- name: GetDailyFilteredCount :one
SELECT COALESCE(SUM(filtered_count), 0) FROM token_usage WHERE user_email = ? AND date = ?;

-- name: GetMonthlyFilteredCount :one
SELECT COALESCE(SUM(filtered_count), 0) FROM token_usage WHERE user_email = ? AND date >= ? AND date < ?;
