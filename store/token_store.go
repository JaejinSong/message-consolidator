package store

import (
	"database/sql"
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
	tokenFlushingData = make(map[string]*tokenData) // DB 기록 중(Flush) 조회 시 누락 방지용
	lastTokenFlush    time.Time

	usageCache   = make(map[string]*tokenUsageCacheData)
	usageCacheMu sync.RWMutex
)

func InitTokenUsageTable() {
	_, err := db.Exec(SQL.InitTokenUsageTable)
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

	// 캐시가 존재하고 날짜/월이 같다면 캐시에도 즉시 더해줍니다 (DB 조회 방어)
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

func FlushTokenUsage() error {
	tokenMu.Lock()
	if len(tokenDirtyData) == 0 {
		tokenMu.Unlock()
		return nil
	}
	tokenFlushingData = tokenDirtyData
	tokenDirtyData = make(map[string]*tokenData)
	tokenMu.Unlock()

	err := WithDBRetry("FlushTokenUsage", func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		stmt, err := tx.Prepare(SQL.UpsertTokenUsage)
		if err != nil {
			return err
		}
		defer stmt.Close()

		today := time.Now().Format("2006-01-02")

		for email, data := range tokenFlushingData {
			totalTokens := data.Prompt + data.Completion
			if _, err := stmt.Exec(email, data.Prompt, data.Completion, totalTokens, today); err != nil {
				return err
			}
		}
		return tx.Commit()
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
	tokenFlushingData = make(map[string]*tokenData) // 성공/실패 여부와 무관하게 비움
	tokenMu.Unlock()

	return err
}

func FlushTokenUsageIfNeeded() {
	tokenMu.Lock()
	shouldFlush := time.Since(lastTokenFlush) > 1*time.Hour && len(tokenDirtyData) > 0
	tokenMu.Unlock()

	if shouldFlush {
		if err := FlushTokenUsage(); err == nil {
			tokenMu.Lock()
			lastTokenFlush = time.Now()
			tokenMu.Unlock()
		}
	}
}

func GetDailyTokenUsage(email string) (int, int, error) {
	today := time.Now().Format("2006-01-02")

	usageCacheMu.RLock()
	if cache, exists := usageCache[email]; exists && cache.Date == today {
		dp, dc := cache.DailyPrompt, cache.DailyCompletion
		usageCacheMu.RUnlock()
		return dp, dc, nil
	}
	usageCacheMu.RUnlock()

	var promptNull, completionNull sql.NullInt64
	err := db.QueryRow(SQL.GetDailyTokenUsage, email, today).Scan(&promptNull, &completionNull)

	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}

	prompt := int(promptNull.Int64)
	completion := int(completionNull.Int64)

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

func GetMonthlyTokenUsage(email string) (int, int, error) {
	currentMonth := time.Now().Format("2006-01")

	usageCacheMu.RLock()
	if cache, exists := usageCache[email]; exists && cache.Month == currentMonth {
		mp, mc := cache.MonthlyPrompt, cache.MonthlyCompletion
		usageCacheMu.RUnlock()
		return mp, mc, nil
	}
	usageCacheMu.RUnlock()
	firstDay := currentMonth + "-01"
	// 현재 달의 1일을 기준으로 다음 달 1일을 계산하여 31일 등에서의 오버플로우 방지
	now := time.Now()
	firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	nextMonthFirstDay := firstOfThisMonth.AddDate(0, 1, 0).Format("2006-01-02")

	var promptNull, completionNull sql.NullInt64
	err := db.QueryRow(SQL.GetMonthlyTokenUsage, email, firstDay, nextMonthFirstDay).Scan(&promptNull, &completionNull)

	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}

	prompt := int(promptNull.Int64)
	completion := int(completionNull.Int64)

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

func SaveGmailToken(email, tokenJSON string) error {
	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	_, err := db.Exec(SQL.UpsertGmailToken,
		email, tokenJSON, tokenJSON)
	return err
}

func GetGmailToken(email string) (string, error) {
	metadataMu.RLock()
	token, ok := tokenCache[email]
	metadataMu.RUnlock()
	if ok {
		return token, nil
	}

	var tokenJSON string
	err := db.QueryRow(SQL.GetGmailToken, email).Scan(&tokenJSON)
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

func DeleteGmailToken(email string) error {
	metadataMu.Lock()
	delete(tokenCache, email)
	metadataMu.Unlock()

	_, err := db.Exec(SQL.DeleteGmailToken, email)
	return err
}
