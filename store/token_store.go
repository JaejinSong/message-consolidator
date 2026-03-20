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

var (
	tokenMu        sync.Mutex
	tokenDirtyData = make(map[string]*tokenData)
	lastTokenFlush time.Time
)

func InitTokenUsageTable() {
	query := `
	CREATE TABLE IF NOT EXISTS token_usage (
		id SERIAL PRIMARY KEY,
		user_email VARCHAR(255) NOT NULL,
		date DATE NOT NULL DEFAULT CURRENT_DATE,
		prompt_tokens INT DEFAULT 0,
		completion_tokens INT DEFAULT 0,
		total_tokens INT DEFAULT 0,
		UNIQUE(user_email, date)
	);`
	_, err := db.Exec(query)
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

	return nil
}

func FlushTokenUsage() error {
	tokenMu.Lock()
	if len(tokenDirtyData) == 0 {
		tokenMu.Unlock()
		return nil
	}
	toFlush := tokenDirtyData
	tokenDirtyData = make(map[string]*tokenData)
	tokenMu.Unlock()

	err := WithDBRetry("FlushTokenUsage", func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		query := `
			INSERT INTO token_usage (user_email, date, prompt_tokens, completion_tokens, total_tokens)
			VALUES ($1, CURRENT_DATE, $2, $3, $4)
			ON CONFLICT (user_email, date)
			DO UPDATE SET 
				prompt_tokens = token_usage.prompt_tokens + EXCLUDED.prompt_tokens,
				completion_tokens = token_usage.completion_tokens + EXCLUDED.completion_tokens,
				total_tokens = token_usage.total_tokens + EXCLUDED.total_tokens;
		`
		stmt, err := tx.Prepare(query)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for email, data := range toFlush {
			totalTokens := data.Prompt + data.Completion
			if _, err := stmt.Exec(email, data.Prompt, data.Completion, totalTokens); err != nil {
				return err
			}
		}
		return tx.Commit()
	})

	if err != nil {
		logger.Errorf("[STORE] Failed to flush token usage: %v", err)
		tokenMu.Lock()
		for email, data := range toFlush {
			if _, ok := tokenDirtyData[email]; !ok {
				tokenDirtyData[email] = &tokenData{}
			}
			tokenDirtyData[email].Prompt += data.Prompt
			tokenDirtyData[email].Completion += data.Completion
		}
		tokenMu.Unlock()
		return err
	}
	return nil
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
	var prompt, completion int
	query := `SELECT COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0) FROM token_usage WHERE user_email = $1 AND date = CURRENT_DATE`
	err := db.QueryRow(query, email).Scan(&prompt, &completion)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}

	tokenMu.Lock()
	if data, ok := tokenDirtyData[email]; ok {
		prompt += data.Prompt
		completion += data.Completion
	}
	tokenMu.Unlock()

	return prompt, completion, nil
}

func SaveGmailToken(email, tokenJSON string) error {
	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	_, err := db.Exec(`
		INSERT INTO gmail_tokens (user_email, token_json, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_email) DO UPDATE SET token_json = $2, updated_at = NOW()`,
		email, tokenJSON)
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
	err := db.QueryRow("SELECT token_json FROM gmail_tokens WHERE user_email = $1", email).Scan(&tokenJSON)
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
