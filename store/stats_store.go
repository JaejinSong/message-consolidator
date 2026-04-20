package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
	"sync"
	"time"
)

import "database/sql"

func GetUserStats(email string, userTz string) (UserStats, error) {
	var stats UserStats
	stats.DailyCompletions = make(map[string]int)
	stats.SourceDistribution = make(map[string]int)
	stats.SourceDistributionTotal = make(map[string]int)
	stats.HourlyActivity = make(map[int]int)

	var wg sync.WaitGroup

	sqliteOffset := GetSQLiteOffset(userTz)

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		totalCount, _ := queries.GetTotalCompleted(context.Background(), email)
		stats.TotalCompleted = int(totalCount)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		userName, _ := queries.GetUserByEmailSimple(context.Background(), nullString(email))
		pendingCount, _ := queries.GetPendingMe(context.Background(), db.GetPendingMeParams{
			Column1: email,
			Column2: userName,
		})
		stats.PendingMe = int(pendingCount)
	}()



	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		rows, err := queries.GetDailyCompletions(context.Background(), db.GetDailyCompletionsParams{
			Strftime:  sqliteOffset,
			UserEmail: nullString(email),
			Datetime:  sqliteOffset,
		})
		if err == nil {
			for _, row := range rows {
				if d, ok := row.D.(string); ok {
					stats.DailyCompletions[d] = int(row.C)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		rows, err := queries.GetHourlyActivity(context.Background(), db.GetHourlyActivityParams{
			Strftime:  sqliteOffset,
			UserEmail: nullString(email),
		})
		if err == nil {
			for _, row := range rows {
				hrStr := ""
				if s, ok := row.Hr.(string); ok {
					hrStr = s
				} else if b, ok := row.Hr.([]byte); ok {
					hrStr = string(b)
				}
				if hrStr != "" {
					var hr int
					fmt.Sscanf(hrStr, "%d", &hr)
					stats.HourlyActivity[hr] = int(row.C)
				}
			}
		}
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		userName, _ := queries.GetUserByEmailSimple(context.Background(), nullString(email))
		thresholdStr := GetLocalThreshold(userTz, 3)
		threshold, _ := time.Parse(time.RFC3339, thresholdStr)
		abandonedCount, _ := queries.GetAbandonedTasks(context.Background(), db.GetAbandonedTasksParams{
			UserEmail: email,
			CreatedAt: sql.NullTime{Time: threshold, Valid: !threshold.IsZero()},
			Assignee:  userName,
		})
		stats.AbandonedTasks = int(abandonedCount)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		userName, _ := queries.GetUserByEmailSimple(context.Background(), nullString(email))
		pendingOthersCount, _ := queries.GetPendingOthers(context.Background(), db.GetPendingOthersParams{
			UserEmail: email,
			Assignee:  userName,
		})
		stats.PendingOthers = int(pendingOthersCount)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		rows, err := queries.GetTaskCountByContactType(context.Background(), email)
		if err == nil {
			for _, row := range rows {
				switch row.ContactType {
				case "internal":
					stats.InternalTaskCount = int(row.Count)
				case "partner":
					stats.PartnerTaskCount = int(row.Count)
				case "customer":
					stats.CustomerTaskCount = int(row.Count)
				default:
					stats.ExternalTaskCount += int(row.Count)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		rows, err := queries.GetSourceDistributionActive(context.Background(), email)
		if err == nil {
			for _, row := range rows {
				stats.SourceDistribution[row.Source] = int(row.Count)
			}
		}
		rowsTotal, err := queries.GetSourceDistributionTotal(context.Background(), email)
		if err == nil {
			for _, row := range rowsTotal {
				stats.SourceDistributionTotal[row.Source] = int(row.Count)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn := GetDB()
		queries := db.New(conn)
		rows, err := queries.GetCompletionHistory(context.Background(), db.GetCompletionHistoryParams{
			Strftime:  sqliteOffset,
			UserEmail: nullString(email),
			Datetime:  sqliteOffset,
		})
		if err == nil {
			var currentData string
			currentCounts := make(map[string]int)
			for _, row := range rows {
				d := ""
				if s, ok := row.CDate.(string); ok {
					d = s
				}
				if d != currentData {
					if currentData != "" {
						stats.CompletionHistory = append(stats.CompletionHistory, TimeSeriesPoint{Date: currentData, Counts: currentCounts})
					}
					currentData = d
					currentCounts = make(map[string]int)
				}
				currentCounts[row.Source.String] = int(row.Count)
			}
			if currentData != "" {
				stats.CompletionHistory = append(stats.CompletionHistory, TimeSeriesPoint{Date: currentData, Counts: currentCounts})
			}
		}
	}()

	wg.Wait()
	return stats, nil
}
