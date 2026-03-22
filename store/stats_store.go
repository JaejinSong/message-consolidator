package store

import (
	"fmt"
)

func GetUserStats(email string) (UserStats, error) {
	var stats UserStats
	stats.DailyCompletions = make(map[string]int)
	stats.SourceDistribution = make(map[string]int)
	stats.HourlyActivity = make(map[int]int)

	// 1. Total Completed & Daily Goal
	var userName string
	_ = db.QueryRow("SELECT name FROM users WHERE email = $1", email).Scan(&userName)

	_ = db.QueryRow("SELECT COUNT(*)::int FROM messages WHERE user_email = $1 AND done = true AND is_deleted = false", email).Scan(&stats.TotalCompleted)
	_ = db.QueryRow("SELECT COUNT(*)::int FROM messages WHERE user_email = $1 AND done = false AND is_deleted = false AND (assignee = $2 OR assignee = 'me')", email, userName).Scan(&stats.PendingMe)
	_ = db.QueryRow("SELECT COALESCE(daily_goal, 5) FROM users WHERE email = $1", email).Scan(&stats.DailyGoal)
	if stats.DailyGoal <= 0 {
		stats.DailyGoal = 5 // Default
	}

	// 2. Daily Completions (Last 30 days)
	rows, err := db.Query(`
		SELECT (TO_CHAR(completed_at, 'YYYY-MM-DD'))::text as d, COUNT(*)::int as c
		FROM messages 
		WHERE user_email = $1 AND done = true AND is_deleted = false 
		AND completed_at > NOW() - INTERVAL '30 days'
		GROUP BY 1 ORDER BY 1`, email)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var d string
			var c int
			if err := rows.Scan(&d, &c); err == nil {
				stats.DailyCompletions[d] = c
			}
		}
	}

	// 3. Hourly Activity (All time)
	rows, err = db.Query(`
		SELECT (EXTRACT(HOUR FROM completed_at))::int as hr, COUNT(*)::int as c
		FROM messages 
		WHERE user_email = $1 AND done = true AND is_deleted = false AND completed_at IS NOT NULL
		GROUP BY 1 ORDER BY 1`, email)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var hr, count int
			if err := rows.Scan(&hr, &count); err == nil {
				stats.HourlyActivity[hr] = count
			}
		}
	}

	// 4. Peak Time (Hour of day) logic
	maxCount := -1
	peakHour := -1
	for h := 0; h < 24; h++ {
		count := stats.HourlyActivity[h]
		if count > maxCount {
			maxCount = count
			peakHour = h
		}
	}
	if peakHour != -1 && maxCount > 0 {
		stats.PeakTime = fmt.Sprintf("%02d:00", peakHour)
	} else {
		stats.PeakTime = "-"
	}

	// 5. Abandoned Tasks (> 3 days)
	_ = db.QueryRow(`
		SELECT COUNT(*)::int FROM messages 
		WHERE user_email = $1 AND done = false AND is_deleted = false 
		AND created_at < NOW() - INTERVAL '3 days'`, email).Scan(&stats.AbandonedTasks)

	// 6. Source Distribution
	rows, err = db.Query(`
		SELECT source, COUNT(*)::int FROM messages 
		WHERE user_email = $1 AND is_deleted = false
		GROUP BY source`, email)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s string
			var c int
			if err := rows.Scan(&s, &c); err == nil {
				stats.SourceDistribution[s] = c
			}
		}
	}

	return stats, nil
}
