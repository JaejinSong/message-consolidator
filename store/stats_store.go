package store

import (
	"fmt"
	"sync"
	"time"
)

// getWorkingDaysAgo calculates the time 'days' working days ago from 'now'.
func getWorkingDaysAgo(days int, now time.Time) time.Time {
	t := now
	for i := 0; i < days; {
		t = t.AddDate(0, 0, -1)
		if t.Weekday() != time.Saturday && t.Weekday() != time.Sunday {
			i++
		}
	}
	return t
}

func GetUserStats(email string, userTz string) (UserStats, error) {
	var stats UserStats
	stats.DailyCompletions = make(map[string]int)
	stats.SourceDistribution = make(map[string]int)
	stats.HourlyActivity = make(map[int]int)

	var wg sync.WaitGroup
	dbTz := userTz
	if dbTz == "" {
		dbTz = "UTC"
	}

	// 1. Total Completed
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := "SELECT COUNT(*)::int FROM messages WHERE user_email = $1 AND done = true AND is_deleted = false"
		_ = db.QueryRow(q, email).Scan(&stats.TotalCompleted)
	}()

	// 2. Pending Me
	wg.Add(1)
	go func() {
		defer wg.Done()
		var userName string
		_ = db.QueryRow("SELECT name FROM users WHERE email = $1", email).Scan(&userName)
		q := "SELECT COUNT(*)::int FROM messages WHERE user_email = $1 AND done = false AND is_deleted = false AND (assignee = $2 OR assignee = 'me')"
		_ = db.QueryRow(q, email, userName).Scan(&stats.PendingMe)
	}()

	// 3. Daily Goal
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := "SELECT COALESCE(daily_goal, 5) FROM users WHERE email = $1"
		_ = db.QueryRow(q, email).Scan(&stats.DailyGoal)
		if stats.DailyGoal <= 0 {
			stats.DailyGoal = 5 // Default
		}
	}()

	// 4. Daily Completions (Last 30 days)
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`
		SELECT (TO_CHAR(completed_at AT TIME ZONE '%s', 'YYYY-MM-DD'))::text as d, COUNT(*)::int as c
		FROM messages 
		WHERE user_email = $1 AND done = true AND is_deleted = false 
		AND completed_at > NOW() - INTERVAL '30 days'
		GROUP BY 1 ORDER BY 1`, dbTz)
		rows, err := db.Query(q, email)
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
	}()

	// 5. Hourly Activity (All time) & Peak Time
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`
		SELECT (EXTRACT(HOUR FROM completed_at AT TIME ZONE '%s'))::int as hr, COUNT(*)::int as c
		FROM messages 
		WHERE user_email = $1 AND done = true AND is_deleted = false AND completed_at IS NOT NULL
		GROUP BY 1 ORDER BY 1`, dbTz)
		rows, err := db.Query(q, email)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var hr, count int
				if err := rows.Scan(&hr, &count); err == nil {
					stats.HourlyActivity[hr] = count
				}
			}
		}
		// Peak Time (Hour of day) logic
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
	}()

	// 6. Abandoned Tasks (> 3 days)
	wg.Add(1)
	go func() {
		defer wg.Done()

		threshold := getWorkingDaysAgo(3, time.Now()).Format(time.RFC3339)
		q := `
		SELECT COUNT(*)::int FROM messages 
		WHERE user_email = $1 AND done = false AND is_deleted = false 
		AND created_at < $2`
		_ = db.QueryRow(q, email, threshold).Scan(&stats.AbandonedTasks)
	}()

	// 7. Source Distribution
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := `
		SELECT source, COUNT(*)::int FROM messages 
		WHERE user_email = $1 AND is_deleted = false
		GROUP BY source`
		rows, err := db.Query(q, email)
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
	}()

	// 8. Completion History (Last 365 days for Anki-style chart)
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`
		SELECT TO_CHAR(completed_at AT TIME ZONE '%s', 'YYYY-MM-DD') as c_date, source, COUNT(*)::int 
		FROM messages 
		WHERE user_email = $1 AND done = true AND is_deleted = false 
		AND completed_at >= NOW() - INTERVAL '365 days'
		GROUP BY 1, 2 ORDER BY 1 ASC`, dbTz)
		rows, err := db.Query(q, email)
		if err == nil {
			defer rows.Close()
			var currentData string
			currentCounts := make(map[string]int)

			for rows.Next() {
				var d, s string
				var c int
				if err := rows.Scan(&d, &s, &c); err == nil {
					if d != currentData {
						if currentData != "" {
							stats.CompletionHistory = append(stats.CompletionHistory, TimeSeriesPoint{Date: currentData, Counts: currentCounts})
						}
						currentData = d
						currentCounts = make(map[string]int)
					}
					currentCounts[s] = c
				}
			}
			if currentData != "" {
				stats.CompletionHistory = append(stats.CompletionHistory, TimeSeriesPoint{Date: currentData, Counts: currentCounts})
			}
		}
	}()

	// Wait for all queries to finish
	wg.Wait()

	return stats, nil
}
