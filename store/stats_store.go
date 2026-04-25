package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
	"sync"
	"time"
)

import "database/sql"

func GetUserStats(ctx context.Context, email string, userTz string) (UserStats, error) {
	var stats UserStats
	stats.DailyCompletions = make(map[string]int)
	stats.SourceDistribution = make(map[string]int)
	stats.SourceDistributionTotal = make(map[string]int)
	stats.HourlyActivity = make(map[int]int)
	sqliteOffset := GetSQLiteOffset(userTz)

	queries := db.New(GetDB())
	var wg sync.WaitGroup
	runStatsQuery(&wg, func() { loadTotalCompleted(ctx, queries, email, &stats) })
	runStatsQuery(&wg, func() { loadPendingMe(ctx, queries, email, &stats) })
	runStatsQuery(&wg, func() { loadDailyCompletions(ctx, queries, email, sqliteOffset, &stats) })
	runStatsQuery(&wg, func() { loadHourlyActivity(ctx, queries, email, sqliteOffset, &stats) })
	runStatsQuery(&wg, func() { loadAbandonedTasks(ctx, queries, email, userTz, &stats) })
	runStatsQuery(&wg, func() { loadPendingOthers(ctx, queries, email, &stats) })
	runStatsQuery(&wg, func() { loadContactTypeBreakdown(ctx, queries, email, &stats) })
	runStatsQuery(&wg, func() { loadSourceDistributions(ctx, queries, email, &stats) })
	runStatsQuery(&wg, func() { loadCompletionHistory(ctx, queries, email, sqliteOffset, &stats) })
	wg.Wait()
	return stats, nil
}

//Why: Each stat panel runs an independent SQL query in parallel; runStatsQuery owns the WaitGroup bookkeeping so callers stay declarative.
func runStatsQuery(wg *sync.WaitGroup, fn func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		fn()
	}()
}

func loadTotalCompleted(ctx context.Context, q *db.Queries, email string, stats *UserStats) {
	totalCount, _ := q.GetTotalCompleted(ctx, email)
	stats.TotalCompleted = int(totalCount)
}

func loadPendingMe(ctx context.Context, q *db.Queries, email string, stats *UserStats) {
	userName, _ := q.GetUserByEmailSimple(ctx, nullString(email))
	pendingCount, _ := q.GetPendingMe(ctx, db.GetPendingMeParams{Column1: email, Column2: userName})
	stats.PendingMe = int(pendingCount)
}

func loadDailyCompletions(ctx context.Context, q *db.Queries, email, offset string, stats *UserStats) {
	rows, err := q.GetDailyCompletions(ctx, db.GetDailyCompletionsParams{Strftime: offset, UserEmail: nullString(email), Datetime: offset})
	if err != nil {
		return
	}
	for _, row := range rows {
		if d, ok := row.D.(string); ok {
			stats.DailyCompletions[d] = int(row.C)
		}
	}
}

func loadHourlyActivity(ctx context.Context, q *db.Queries, email, offset string, stats *UserStats) {
	rows, err := q.GetHourlyActivity(ctx, db.GetHourlyActivityParams{Strftime: offset, UserEmail: nullString(email)})
	if err == nil {
		for _, row := range rows {
			if hr := parseHourString(row.Hr); hr >= 0 {
				stats.HourlyActivity[hr] = int(row.C)
			}
		}
	}
	stats.PeakTime = computePeakTime(stats.HourlyActivity)
}

// any 사유: SQLite driver는 strftime/cast 결과를 string 또는 []byte로 임의 반환 — type switch로 정규화.
func parseHourString(raw any) int {
	hrStr := ""
	switch v := raw.(type) {
	case string:
		hrStr = v
	case []byte:
		hrStr = string(v)
	}
	if hrStr == "" {
		return -1
	}
	var hr int
	if _, err := fmt.Sscanf(hrStr, "%d", &hr); err != nil {
		return -1
	}
	return hr
}

func computePeakTime(activity map[int]int) string {
	maxCount, peakHour := -1, -1
	for h := 0; h < 24; h++ {
		if c := activity[h]; c > maxCount {
			maxCount = c
			peakHour = h
		}
	}
	if peakHour < 0 || maxCount <= 0 {
		return "-"
	}
	return fmt.Sprintf("%02d:00", peakHour)
}

func loadAbandonedTasks(ctx context.Context, q *db.Queries, email, tz string, stats *UserStats) {
	userName, _ := q.GetUserByEmailSimple(ctx, nullString(email))
	thresholdStr := GetLocalThreshold(tz, 3)
	threshold, _ := time.Parse(time.RFC3339, thresholdStr)
	abandonedCount, _ := q.GetAbandonedTasks(ctx, db.GetAbandonedTasksParams{
		UserEmail: email,
		CreatedAt: sql.NullTime{Time: threshold, Valid: !threshold.IsZero()},
		Assignee:  userName,
	})
	stats.AbandonedTasks = int(abandonedCount)
}

func loadPendingOthers(ctx context.Context, q *db.Queries, email string, stats *UserStats) {
	userName, _ := q.GetUserByEmailSimple(ctx, nullString(email))
	pendingOthersCount, _ := q.GetPendingOthers(ctx, db.GetPendingOthersParams{UserEmail: email, Assignee: userName})
	stats.PendingOthers = int(pendingOthersCount)
}

func loadContactTypeBreakdown(ctx context.Context, q *db.Queries, email string, stats *UserStats) {
	rows, err := q.GetTaskCountByContactType(ctx, email)
	if err != nil {
		return
	}
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

func loadSourceDistributions(ctx context.Context, q *db.Queries, email string, stats *UserStats) {
	if rows, err := q.GetSourceDistributionActive(ctx, email); err == nil {
		for _, row := range rows {
			stats.SourceDistribution[row.Source] = int(row.Count)
		}
	}
	if rowsTotal, err := q.GetSourceDistributionTotal(ctx, email); err == nil {
		for _, row := range rowsTotal {
			stats.SourceDistributionTotal[row.Source] = int(row.Count)
		}
	}
}

func loadCompletionHistory(ctx context.Context, q *db.Queries, email, offset string, stats *UserStats) {
	rows, err := q.GetCompletionHistory(ctx, db.GetCompletionHistoryParams{Strftime: offset, UserEmail: nullString(email), Datetime: offset})
	if err != nil {
		return
	}
	stats.CompletionHistory = buildCompletionHistory(rows)
}

//Why: Folds the per-row CompletionHistory aggregation out of the goroutine to keep nesting shallow; rows arrive sorted by date.
func buildCompletionHistory(rows []db.GetCompletionHistoryRow) []TimeSeriesPoint {
	var points []TimeSeriesPoint
	var currentDate string
	currentCounts := make(map[string]int)
	for _, row := range rows {
		d := ""
		if s, ok := row.CDate.(string); ok {
			d = s
		}
		if d != currentDate {
			if currentDate != "" {
				points = append(points, TimeSeriesPoint{Date: currentDate, Counts: currentCounts})
			}
			currentDate = d
			currentCounts = make(map[string]int)
		}
		currentCounts[row.Source.String] = int(row.Count)
	}
	if currentDate != "" {
		points = append(points, TimeSeriesPoint{Date: currentDate, Counts: currentCounts})
	}
	return points
}
