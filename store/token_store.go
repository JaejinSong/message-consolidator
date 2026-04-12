package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
	"message-consolidator/logger"
	"sync"
	"time"
)

type tokenData struct {
	Prompt     int
	Completion int
}

type tokenUsageCacheData struct {
	Date              string
	Month             string
	DailyPrompt       int
	DailyCompletion   int
	MonthlyPrompt     int
	MonthlyCompletion int
}

var (
	tokenMu           sync.Mutex
	tokenDirtyData    = make(map[string]*tokenData)
	tokenFlushingData = make(map[string]*tokenData) //Why: Buffers data currently being flushed to the database to prevent data loss if a parallel write occurs.
	lastTokenFlush    time.Time

	usageCache   = make(map[string]*tokenUsageCacheData)
	usageCacheMu sync.RWMutex
)

func InitTokenUsageTable(q Querier) {
	if q == nil {
		q = GetDB()
	}
	// Why: Fallback to manual DDL for self-healing, as sqlc doesn't manage DDL execution directly.
	query := `CREATE TABLE IF NOT EXISTS token_usage (
		email TEXT NOT NULL,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		usage_date TEXT NOT NULL,
		PRIMARY KEY (email, usage_date)
	);`
	_, err := q.Exec(query)
	if err != nil {
		logger.Errorf("Failed to initialize token_usage table: %v", err)
	}
}

func AddTokenUsage(email string, promptTokens, completionTokens int) error {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if _, ok := tokenDirtyData[email]; !ok {
		tokenDirtyData[email] = &tokenData{}
	}
	tokenDirtyData[email].Prompt += promptTokens
	tokenDirtyData[email].Completion += completionTokens

	//Why: Proactively updates the in-memory usage cache to avoid redundant database reads for the current period.
	today := time.Now().Format("2006-01-02")
	currentMonth := time.Now().Format("2006-01")

	usageCacheMu.Lock()
	if cache, ok := usageCache[email]; ok {
		if cache.Date == today {
			cache.DailyPrompt += promptTokens
			cache.DailyCompletion += completionTokens
		}
		if cache.Month == currentMonth {
			cache.MonthlyPrompt += promptTokens
			cache.MonthlyCompletion += completionTokens
		}
	}
	usageCacheMu.Unlock()

	return nil
}

func FlushTokenUsage(ctx context.Context) error {
	tokenMu.Lock()
	if len(tokenDirtyData) == 0 {
		tokenMu.Unlock()
		return nil
	}
	tokenFlushingData = tokenDirtyData
	tokenDirtyData = make(map[string]*tokenData)
	tokenMu.Unlock()

	err := WithDBRetry("FlushTokenUsage", func() error {
		conn := GetDB()
		queries := db.New(conn)
		today := time.Now().Format("2006-01-02")

		for email, data := range tokenFlushingData {
			totalTokens := data.Prompt + data.Completion
			parsedDate, _ := time.Parse("2006-01-02", today)
			err := queries.UpsertTokenUsage(ctx, db.UpsertTokenUsageParams{
				UserEmail:        email,
				PromptTokens:     sql.NullInt64{Int64: int64(data.Prompt), Valid: true},
				CompletionTokens: sql.NullInt64{Int64: int64(data.Completion), Valid: true},
				TotalTokens:      sql.NullInt64{Int64: int64(totalTokens), Valid: true},
				Date:             parsedDate,
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
		for email, data := range tokenFlushingData {
			if _, ok := tokenDirtyData[email]; !ok {
				tokenDirtyData[email] = &tokenData{}
			}
			tokenDirtyData[email].Prompt += data.Prompt
			tokenDirtyData[email].Completion += data.Completion
		}
	}
	tokenFlushingData = make(map[string]*tokenData)
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

func GetDailyTokenUsage(ctx context.Context, email string) (int, int, error) {
	today := time.Now().Format("2006-01-02")

	usageCacheMu.RLock()
	if cache, exists := usageCache[email]; exists && cache.Date == today {
		dp, dc := cache.DailyPrompt, cache.DailyCompletion
		usageCacheMu.RUnlock()
		return dp, dc, nil
	}
	usageCacheMu.RUnlock()

	conn := GetDB()
	queries := db.New(conn)
	parsedDate, _ := time.Parse("2006-01-02", today)
	row, err := queries.GetDailyTokenUsage(ctx, db.GetDailyTokenUsageParams{
		UserEmail: email,
		Date:      parsedDate,
	})

	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}

	// sqlc COALESCE(SUM(...)) returns interface{} which is float64 or int64 depending on driver
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

	tokenMu.Lock()
	if data, ok := tokenDirtyData[email]; ok {
		prompt += data.Prompt
		completion += data.Completion
	}
	if data, ok := tokenFlushingData[email]; ok {
		prompt += data.Prompt
		completion += data.Completion
	}
	tokenMu.Unlock()

	usageCacheMu.Lock()
	if usageCache[email] == nil {
		usageCache[email] = &tokenUsageCacheData{}
	}
	usageCache[email].Date = today
	usageCache[email].DailyPrompt = prompt
	usageCache[email].DailyCompletion = completion
	usageCacheMu.Unlock()

	return prompt, completion, nil
}

func GetMonthlyTokenUsage(ctx context.Context, email string) (int, int, error) {
	currentMonth := time.Now().Format("2006-01")

	usageCacheMu.RLock()
	if cache, exists := usageCache[email]; exists && cache.Month == currentMonth {
		mp, mc := cache.MonthlyPrompt, cache.MonthlyCompletion
		usageCacheMu.RUnlock()
		return mp, mc, nil
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

	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
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

	tokenMu.Lock()
	if data, ok := tokenDirtyData[email]; ok {
		prompt += data.Prompt
		completion += data.Completion
	}
	if data, ok := tokenFlushingData[email]; ok {
		prompt += data.Prompt
		completion += data.Completion
	}
	tokenMu.Unlock()

	usageCacheMu.Lock()
	if usageCache[email] == nil {
		usageCache[email] = &tokenUsageCacheData{}
	}
	usageCache[email].Month = currentMonth
	usageCache[email].MonthlyPrompt = prompt
	usageCache[email].MonthlyCompletion = completion
	usageCacheMu.Unlock()

	return prompt, completion, nil
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
