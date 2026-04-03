package store

import (
	"fmt"
	"sync"
)

func GetUserStats(email string, userTz string) (UserStats, error) {
	var stats UserStats
	stats.DailyCompletions = make(map[string]int)
	stats.SourceDistribution = make(map[string]int)
	stats.SourceDistributionTotal = make(map[string]int)
	stats.HourlyActivity = make(map[int]int)

	var wg sync.WaitGroup

	//Why: Calculates the SQLite offset based on the user's timezone to ensure consistent date aggregation.
	sqliteOffset := GetSQLiteOffset(userTz)

	//Why: Retrieves the total count of completed tasks for the high-level stats overview.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(SQL.GetTotalCompleted, email).Scan(&stats.TotalCompleted)
	}()

	//Why: Fetches the count of tasks that are still pending action by the current user.
	wg.Add(1)
	go func() {
		defer wg.Done()
		var userName string
		_ = db.QueryRow(SQL.GetUserByEmailSimple, email).Scan(&userName)
		_ = db.QueryRow(SQL.GetPendingMe, email, userName).Scan(&stats.PendingMe)
	}()

	//Why: Loads the user's daily task completion goal, defaulting to 5 if not set.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(SQL.GetDailyGoal, email).Scan(&stats.DailyGoal)
		if stats.DailyGoal <= 0 {
			stats.DailyGoal = 5 // Default
		}
	}()

	//Why: Aggregates task completions by day for the last 30 days to populate activity charts.
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, err := db.Query(SQL.GetDailyCompletions, sqliteOffset, email, sqliteOffset)
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

	//Why: Distributes task completions by hour of the day and identifies peak productivity windows.
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, err := db.Query(SQL.GetHourlyActivity, sqliteOffset, email)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var hr, count int
				if err := rows.Scan(&hr, &count); err == nil {
					stats.HourlyActivity[hr] = count
				}
			}
		}
		//Why: Identifies the hour with the highest completion count to label as the peak productivity time.
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

	//Why: Counts tasks that have been inactive for more than three days, excluding personal tasks.
	wg.Add(1)
	go func() {
		defer wg.Done()
		var userName string
		_ = db.QueryRow(SQL.GetUserByEmailSimple, email).Scan(&userName)
		threshold := GetLocalThreshold(userTz, 3)
		_ = db.QueryRow(SQL.GetAbandonedTasks, email, threshold, userName).Scan(&stats.AbandonedTasks)
	}()

	//Why: Counts incomplete tasks assigned to others (SSOT for frontend tabs).
	wg.Add(1)
	go func() {
		defer wg.Done()
		var userName string
		_ = db.QueryRow(SQL.GetUserByEmailSimple, email).Scan(&userName)
		_ = db.QueryRow(SQL.GetPendingOthers, email, userName).Scan(&stats.PendingOthers)
	}()

	//Why: Counts tasks in 'waiting' category for the dashboard widget.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(SQL.GetWaitingTasks, email).Scan(&stats.WaitingTasks)
	}()

	//Why: Calculates the distribution of tasks across different communication sources (Active & Total).
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Active
		rows, err := db.Query(SQL.GetSourceDistributionActive, email)
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
		// Total (including archive)
		rowsTotal, err := db.Query(SQL.GetSourceDistributionTotal, email)
		if err == nil {
			defer rowsTotal.Close()
			for rowsTotal.Next() {
				var s string
				var c int
				if err := rowsTotal.Scan(&s, &c); err == nil {
					stats.SourceDistributionTotal[s] = c
				}
			}
		}
	}()

	//Why: Builds a detailed 365-day completion history to populate the contribution heatmap (Anki-style chart).
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows, err := db.Query(SQL.GetCompletionHistory, sqliteOffset, email, sqliteOffset)
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

	//Why: Synchronizes the completion of all parallel database queries before returning the aggregated stats.
	wg.Wait()

	return stats, nil
}
