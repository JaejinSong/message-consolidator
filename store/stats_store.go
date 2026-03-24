package store

import (
	"fmt"
	"sync"
)


func GetUserStats(email string, userTz string) (UserStats, error) {
	var stats UserStats
	stats.DailyCompletions = make(map[string]int)
	stats.SourceDistribution = make(map[string]int)
	stats.HourlyActivity = make(map[int]int)

	var wg sync.WaitGroup
	
	// Centralized SQLite offset calculation
	sqliteOffset := GetSQLiteOffset(userTz)

	// 1. Total Completed
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := "SELECT COUNT(*) FROM messages WHERE user_email = ? AND done = 1 AND is_deleted = 0"
		_ = db.QueryRow(q, email).Scan(&stats.TotalCompleted)
	}()

	// 2. Pending Me
	wg.Add(1)
	go func() {
		defer wg.Done()
		var userName string
		_ = db.QueryRow("SELECT name FROM users WHERE email = ?", email).Scan(&userName)
		q := "SELECT COUNT(*) FROM messages WHERE user_email = ? AND done = 0 AND is_deleted = 0 AND (assignee = ? OR assignee = 'me')"
		_ = db.QueryRow(q, email, userName).Scan(&stats.PendingMe)
	}()

	// 3. Daily Goal
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := "SELECT COALESCE(daily_goal, 5) FROM users WHERE email = ?"
		_ = db.QueryRow(q, email).Scan(&stats.DailyGoal)
		if stats.DailyGoal <= 0 {
			stats.DailyGoal = 5 // Default
		}
	}()

	// 4. Daily Completions (Last 30 days)
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := `
		SELECT strftime('%Y-%m-%d', completed_at, ?) as d, COUNT(*) as c
		FROM messages 
		WHERE user_email = ? AND done = 1 AND is_deleted = 0 
		AND completed_at > datetime('now', ?, '-30 days')
		GROUP BY 1 ORDER BY 1`
		rows, err := db.Query(q, sqliteOffset, email, sqliteOffset)
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
		q := `
		SELECT strftime('%H', completed_at, ?) as hr, COUNT(*) as c
		FROM messages 
		WHERE user_email = ? AND done = 1 AND is_deleted = 0 AND completed_at IS NOT NULL
		GROUP BY 1 ORDER BY 1`
		rows, err := db.Query(q, sqliteOffset, email)
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

	// 6. Abandoned Tasks (> 3 days) - Excluding tasks assigned to 'me' or the user's name
	wg.Add(1)
	go func() {
		defer wg.Done()
		var userName string
		_ = db.QueryRow("SELECT name FROM users WHERE email = ?", email).Scan(&userName)

		threshold := GetLocalThreshold(userTz, 3)
		
		q := `
		SELECT COUNT(*) FROM messages 
		WHERE user_email = ? AND done = 0 AND is_deleted = 0 
		AND created_at < ? AND (assignee != ? AND assignee != 'me')`
		_ = db.QueryRow(q, email, threshold, userName).Scan(&stats.AbandonedTasks)
	}()


	// 7. Source Distribution
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := `
		SELECT source, COUNT(*) FROM messages 
		WHERE user_email = ? AND is_deleted = 0
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
		q := `
		SELECT strftime('%Y-%m-%d', completed_at) as c_date, source, COUNT(*) 
		FROM messages 
		WHERE user_email = ? AND done = 1 AND is_deleted = 0 
		AND completed_at >= datetime('now', '-365 days')
		GROUP BY 1, 2 ORDER BY 1 ASC`
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
