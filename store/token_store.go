package store

import (
	"context"
	"database/sql"
	"errors"
	"message-consolidator/db"
	"message-consolidator/logger"
	"sync"
	"time"
)

// tokenBucket keys in-memory token data by the same dimensions used in the DB UNIQUE
// constraint, so per-(step, model, source, report_id) breakdown survives buffering and
// flushing. ReportID=0 = un-attributed (default for non-report calls).
type tokenBucket struct {
	Email    string
	Step     string
	Model    string
	Source   string
	ReportID ReportID
}

type tokenData struct {
	Prompt     int
	Completion int
	Calls      int
	Filtered   int //Why: Filtered count is a per-user counter, written only to the legacy bucket (step="", model="", source="").
}

type tokenUsageCacheData struct {
	Date              string
	Month             string
	DailyPrompt       int
	DailyCompletion   int
	DailyFiltered     int
	MonthlyPrompt     int
	MonthlyCompletion int
	MonthlyFiltered   int
}

var (
	tokenMu           sync.Mutex
	tokenDirtyData    = make(map[tokenBucket]*tokenData)
	tokenFlushingData = make(map[tokenBucket]*tokenData) //Why: Buffers data currently being flushed to the database to prevent data loss if a parallel write occurs.
	lastTokenFlush    time.Time

	usageCache   = make(map[string]*tokenUsageCacheData)
	usageCacheMu sync.RWMutex
)

// AddTokenUsage records token consumption for a single AI call, attributed to a specific
// (step, model, source, report_id) bucket. Use step="" for unattributed legacy calls and
// reportID=0 for AI calls not bound to a specific report.
func AddTokenUsage(email, step, model, source string, reportID ReportID, promptTokens, completionTokens int) error {
	key := tokenBucket{Email: email, Step: step, Model: model, Source: source, ReportID: reportID}

	tokenMu.Lock()
	if _, ok := tokenDirtyData[key]; !ok {
		tokenDirtyData[key] = &tokenData{}
	}
	tokenDirtyData[key].Prompt += promptTokens
	tokenDirtyData[key].Completion += completionTokens
	tokenDirtyData[key].Calls++
	tokenMu.Unlock()

	updateUsageCache(email, promptTokens, completionTokens, 0)
	return nil
}

// IncrementFilteredCount increments the filtered message count for the user.
// Why: [Performance] Caches the filtered count in-memory to minimize DB writes for high-frequency noise rejection.
func IncrementFilteredCount(email string) {
	key := tokenBucket{Email: email}

	tokenMu.Lock()
	if _, ok := tokenDirtyData[key]; !ok {
		tokenDirtyData[key] = &tokenData{}
	}
	tokenDirtyData[key].Filtered++
	tokenMu.Unlock()

	updateUsageCache(email, 0, 0, 1)
}

// updateUsageCache keeps the daily/monthly aggregates hot so GetDailyTokenUsage/GetMonthlyTokenUsage
// can answer without a DB round-trip between flushes.
func updateUsageCache(email string, prompt, completion, filtered int) {
	today := time.Now().Format("2006-01-02")
	currentMonth := time.Now().Format("2006-01")

	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()
	cache, ok := usageCache[email]
	if !ok {
		return
	}
	if cache.Date == today {
		cache.DailyPrompt += prompt
		cache.DailyCompletion += completion
		cache.DailyFiltered += filtered
	}
	if cache.Month == currentMonth {
		cache.MonthlyPrompt += prompt
		cache.MonthlyCompletion += completion
		cache.MonthlyFiltered += filtered
	}
}

func FlushTokenUsage(ctx context.Context) error {
	tokenMu.Lock()
	if len(tokenDirtyData) == 0 {
		tokenMu.Unlock()
		return nil
	}
	tokenFlushingData = tokenDirtyData
	tokenDirtyData = make(map[tokenBucket]*tokenData)
	tokenMu.Unlock()

	err := WithDBRetry("FlushTokenUsage", func() error {
		conn := GetDB()
		queries := db.New(conn)
		parsedDate, _ := time.Parse("2006-01-02", time.Now().Format("2006-01-02"))

		for key, data := range tokenFlushingData {
			totalTokens := data.Prompt + data.Completion
			err := queries.UpsertTokenUsage(ctx, db.UpsertTokenUsageParams{
				UserEmail:        key.Email,
				Date:             parsedDate,
				Step:             key.Step,
				Model:            key.Model,
				Source:           key.Source,
				ReportID:         int64(key.ReportID),
				PromptTokens:     sql.NullInt64{Int64: int64(data.Prompt), Valid: true},
				CompletionTokens: sql.NullInt64{Int64: int64(data.Completion), Valid: true},
				TotalTokens:      sql.NullInt64{Int64: int64(totalTokens), Valid: true},
				CallCount:        sql.NullInt64{Int64: int64(data.Calls), Valid: true},
				FilteredCount:    sql.NullInt64{Int64: int64(data.Filtered), Valid: true},
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	tokenMu.Lock()
	if err != nil {
		logger.Errorf("[STORE] Failed to flush token usage: %v", err)
		for key, data := range tokenFlushingData {
			if _, ok := tokenDirtyData[key]; !ok {
				tokenDirtyData[key] = &tokenData{}
			}
			tokenDirtyData[key].Prompt += data.Prompt
			tokenDirtyData[key].Completion += data.Completion
			tokenDirtyData[key].Calls += data.Calls
			tokenDirtyData[key].Filtered += data.Filtered
		}
	}
	tokenFlushingData = make(map[tokenBucket]*tokenData)
	tokenMu.Unlock()

	return err
}

func FlushTokenUsageIfNeeded(ctx context.Context) {
	tokenMu.Lock()
	shouldFlush := time.Since(lastTokenFlush) > 1*time.Hour && len(tokenDirtyData) > 0
	tokenMu.Unlock()

	if shouldFlush {
		if err := FlushTokenUsage(ctx); err == nil {
			tokenMu.Lock()
			lastTokenFlush = time.Now()
			tokenMu.Unlock()
		}
	}
}

// getInMemoryUsage gathers pending token usage from dirty and flushing buffers,
// summing across every (step, model, source) bucket that belongs to the email.
// Why: [Performance] Shared logic for daily/monthly stats that ensures thread safety.
func getInMemoryUsage(email string) (int, int, int) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	p, c, f := 0, 0, 0
	sum := func(buf map[tokenBucket]*tokenData) {
		for key, data := range buf {
			if key.Email != email {
				continue
			}
			p += data.Prompt
			c += data.Completion
			f += data.Filtered
		}
	}
	sum(tokenDirtyData)
	sum(tokenFlushingData)
	return p, c, f
}

func GetDailyTokenUsage(ctx context.Context, email string) (int, int, int, error) {
	today := time.Now().Format("2006-01-02")

	usageCacheMu.RLock()
	if cache, exists := usageCache[email]; exists && cache.Date == today {
		dp, dc, df := cache.DailyPrompt, cache.DailyCompletion, cache.DailyFiltered
		usageCacheMu.RUnlock()
		return dp, dc, df, nil
	}
	usageCacheMu.RUnlock()

	conn := GetDB()
	queries := db.New(conn)
	parsedDate, _ := time.Parse("2006-01-02", today)
	row, err := queries.GetDailyTokenUsage(ctx, db.GetDailyTokenUsageParams{
		UserEmail: email,
		Date:      parsedDate,
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, err
	}

	// any 사유: sqlc COALESCE(SUM(...))는 driver별로 float64 또는 int64로 반환 — 양쪽 모두 normalize.
	prompt := 0
	if val, ok := row.Coalesce.(int64); ok {
		prompt = int(val)
	} else if val, ok := row.Coalesce.(float64); ok {
		prompt = int(val)
	}

	completion := 0
	if val, ok := row.Coalesce_2.(int64); ok {
		completion = int(val)
	} else if val, ok := row.Coalesce_2.(float64); ok {
		completion = int(val)
	}

	filteredCount := 0
	if val, ok := row.Coalesce_3.(int64); ok {
		filteredCount = int(val)
	} else if val, ok := row.Coalesce_3.(float64); ok {
		filteredCount = int(val)
	}

	ip, ic, ifl := getInMemoryUsage(email)
	prompt += ip
	completion += ic
	filteredCount += ifl

	usageCacheMu.Lock()
	if usageCache[email] == nil {
		usageCache[email] = &tokenUsageCacheData{}
	}
	usageCache[email].Date = today
	usageCache[email].DailyPrompt = prompt
	usageCache[email].DailyCompletion = completion
	usageCache[email].DailyFiltered = filteredCount
	usageCacheMu.Unlock()

	return prompt, completion, filteredCount, nil
}

func GetMonthlyTokenUsage(ctx context.Context, email string) (int, int, int, error) {
	currentMonth := time.Now().Format("2006-01")

	usageCacheMu.RLock()
	if cache, exists := usageCache[email]; exists && cache.Month == currentMonth {
		mp, mc, mf := cache.MonthlyPrompt, cache.MonthlyCompletion, cache.MonthlyFiltered
		usageCacheMu.RUnlock()
		return mp, mc, mf, nil
	}
	usageCacheMu.RUnlock()
	firstDay := currentMonth + "-01"
	//Why: Safely calculates the boundary for the next month to avoid date overflow issues that occur on the 31st of certain months.
	now := time.Now()
	firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	nextMonthFirstDay := firstOfThisMonth.AddDate(0, 1, 0).Format("2006-01-02")

	conn := GetDB()
	queries := db.New(conn)
	pFirstDay, _ := time.Parse("2006-01-02", firstDay)
	pNextMonth, _ := time.Parse("2006-01-02", nextMonthFirstDay)
	row, err := queries.GetMonthlyTokenUsage(ctx, db.GetMonthlyTokenUsageParams{
		UserEmail: email,
		Date:      pFirstDay,
		Date_2:    pNextMonth,
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, err
	}

	prompt := 0
	if val, ok := row.Coalesce.(int64); ok {
		prompt = int(val)
	} else if val, ok := row.Coalesce.(float64); ok {
		prompt = int(val)
	}

	completion := 0
	if val, ok := row.Coalesce_2.(int64); ok {
		completion = int(val)
	} else if val, ok := row.Coalesce_2.(float64); ok {
		completion = int(val)
	}

	filteredCount := 0
	if val, ok := row.Coalesce_3.(int64); ok {
		filteredCount = int(val)
	} else if val, ok := row.Coalesce_3.(float64); ok {
		filteredCount = int(val)
	}

	ip, ic, ifl := getInMemoryUsage(email)
	prompt += ip
	completion += ic
	filteredCount += ifl

	usageCacheMu.Lock()
	if usageCache[email] == nil {
		usageCache[email] = &tokenUsageCacheData{}
	}
	usageCache[email].Month = currentMonth
	usageCache[email].MonthlyPrompt = prompt
	usageCache[email].MonthlyCompletion = completion
	usageCache[email].MonthlyFiltered = filteredCount
	usageCacheMu.Unlock()

	return prompt, completion, filteredCount, nil
}

// ReportTokenCost is the per-report aggregate of prompt/completion tokens and call count
// across all report-bound steps (ReportSummary, ReportVizData, TranslateReport, ...).
type ReportTokenCost struct {
	PromptTokens     int
	CompletionTokens int
	CallCount        int
}

// GetReportTokenUsage returns DB-flushed + in-memory token totals for a single report.
// Mirrors the DB+buffer sum pattern used by GetDailyTokenUsage so callers see real-time cost
// even before the hourly flush.
func GetReportTokenUsage(ctx context.Context, reportID ReportID) (ReportTokenCost, error) {
	conn := GetDB()
	queries := db.New(conn)
	row, err := queries.GetReportTokenUsage(ctx, int64(reportID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReportTokenCost{}, err
	}

	cost := ReportTokenCost{
		PromptTokens:     coalesceInt(row.PromptTokens),
		CompletionTokens: coalesceInt(row.CompletionTokens),
		CallCount:        coalesceInt(row.CallCount),
	}

	tokenMu.Lock()
	defer tokenMu.Unlock()
	addBuf := func(buf map[tokenBucket]*tokenData) {
		for key, data := range buf {
			if key.ReportID != reportID {
				continue
			}
			cost.PromptTokens += data.Prompt
			cost.CompletionTokens += data.Completion
			cost.CallCount += data.Calls
		}
	}
	addBuf(tokenDirtyData)
	addBuf(tokenFlushingData)
	return cost, nil
}

// coalesceInt normalizes sqlc COALESCE(SUM(...)) results — driver returns int64 or float64.
// any 사유: sqlc COALESCE(SUM(...))는 driver별로 float64 또는 int64로 반환.
func coalesceInt(v any) int {
	switch x := v.(type) {
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func SaveGmailToken(ctx context.Context, email, tokenJSON string) error {
	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	conn := GetDB()
	queries := db.New(conn)
	err := queries.UpsertGmailToken(ctx, db.UpsertGmailTokenParams{
		UserEmail: email,
		TokenJson: tokenJSON,
	})
	return err
}

func GetGmailToken(ctx context.Context, email string) (string, error) {
	metadataMu.RLock()
	token, ok := tokenCache[email]
	metadataMu.RUnlock()
	if ok {
		return token, nil
	}

	conn := GetDB()
	queries := db.New(conn)
	tokenJSON, err := queries.GetGmailToken(ctx, email)
	if err != nil {
		return "", err
	}

	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	return tokenJSON, nil
}

func HasGmailToken(email string) bool {
	metadataMu.RLock()
	_, ok := tokenCache[email]
	metadataMu.RUnlock()
	return ok
}

func DeleteGmailToken(ctx context.Context, email string) error {
	metadataMu.Lock()
	delete(tokenCache, email)
	metadataMu.Unlock()

	conn := GetDB()
	queries := db.New(conn)
	err := queries.DeleteGmailToken(ctx, email)
	return err
}
