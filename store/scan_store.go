package store

import (
	"fmt"
	"message-consolidator/logger"
	"strings"
	"time"
)

// WithDBRetry executes a database operation with exponential backoff,
// designed to gracefully handle serverless DB (NeonDB) cold starts.
func WithDBRetry(operationName string, fn func() error) error {
	var err error
	maxRetries := 3
	for i := 1; i <= maxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		logger.Warnf("[DB-RETRY] %s failed (attempt %d/%d). DB waking up? Err: %v", operationName, i, maxRetries, err)
		time.Sleep(time.Duration(i*2) * time.Second) // Wait 2s, 4s, 6s
	}
	return err
}

func LoadMetadata() error {
	metadataMu.Lock()
	defer metadataMu.Unlock()

	logger.Infof("[CACHE] Initializing metadata cache from DB...")

	// 1. Load Users (TRIM name for consistent mapping)
	rows, err := db.Query("SELECT id, email, COALESCE(TRIM(name), ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at FROM users")
	if err != nil {
		return fmt.Errorf("failed to load users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt); err != nil {
			return fmt.Errorf("scan user failed: %w", err)
		}
		userCache[u.Email] = &u
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error in users: %w", err)
	}

	// 2. Load User Aliases
	aliasRows, err := db.Query("SELECT user_id, alias_name FROM user_aliases")
	if err != nil {
		return fmt.Errorf("failed to load user aliases: %w", err)
	}
	defer aliasRows.Close()

	for aliasRows.Next() {
		var userID int
		var alias string
		if err := aliasRows.Scan(&userID, &alias); err != nil {
			return fmt.Errorf("scan alias failed: %w", err)
		}
		aliasCache[userID] = append(aliasCache[userID], alias)
	}

	// 2.1 Ensure all users have an entry in aliasCache to prevent DB hits
	for _, u := range userCache {
		if _, ok := aliasCache[u.ID]; !ok {
			aliasCache[u.ID] = []string{}
		}
	}

	// 3. Load Scan Metadata
	logger.Infof("[SCAN] Loading existing scan metadata into memory...")
	scanRows, err := db.Query("SELECT user_email, source, target_id, last_ts FROM scan_metadata")
	if err != nil {
		return fmt.Errorf("failed to load scan metadata: %w", err)
	}
	defer scanRows.Close()

	for scanRows.Next() {
		var email, source, targetID, lastTS string
		if err := scanRows.Scan(&email, &source, &targetID, &lastTS); err != nil {
			continue
		}
		key := fmt.Sprintf("%s:%s:%s", email, source, targetID)
		scanCache[key] = lastTS
	}
	logger.Infof("[SCAN] Loaded %d scan metadata entries.", len(scanCache))

	// 4. Load Gmail Tokens
	logger.Infof("[SCAN] Loading existing gmail tokens into memory...")
	tokenRows, err := db.Query("SELECT user_email, token_json FROM gmail_tokens")
	if err != nil {
		return fmt.Errorf("failed to load gmail tokens: %w", err)
	}
	defer tokenRows.Close()

	for tokenRows.Next() {
		var email, token string
		if err := tokenRows.Scan(&email, &token); err != nil {
			return fmt.Errorf("scan gmail token failed: %w", err)
		}
		tokenCache[email] = token
	}

	// 5. Load Tenant Aliases
	tenantRows, err := db.Query("SELECT user_email, original_name, primary_name FROM tenant_aliases")
	if err != nil {
		return fmt.Errorf("failed to load tenant aliases: %w", err)
	}
	defer tenantRows.Close()

	for tenantRows.Next() {
		var email, original, primary string
		if err := tenantRows.Scan(&email, &original, &primary); err != nil {
			continue
		}
		if _, ok := tenantAliasCache[email]; !ok {
			tenantAliasCache[email] = make(map[string]string)
		}
		tenantAliasCache[email][original] = primary
	}

	// 6. Load Contacts
	contactRows, err := db.Query("SELECT user_email, rep_name, aliases FROM contacts")
	if err == nil {
		defer contactRows.Close()
		for contactRows.Next() {
			var email, repName, aliases string
			if err := contactRows.Scan(&email, &repName, &aliases); err == nil {
				contactsCache[email] = append(contactsCache[email], AliasMapping{RepName: repName, Aliases: aliases})
			}
		}
	}

	logger.Infof("[CACHE] Loaded %d users, %d scan entries, %d tokens, %d tenant aliases, %d contact mappings.", len(userCache), len(scanCache), len(tokenCache), len(tenantAliasCache), len(contactsCache))
	return nil
}

func GetLastScan(userEmail, source, targetID string) string {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	key := fmt.Sprintf("%s:%s:%s", userEmail, source, targetID)
	return scanCache[key]
}

func UpdateLastScan(userEmail, source, targetID, ts string) error {
	metadataMu.Lock()
	key := fmt.Sprintf("%s:%s:%s", userEmail, source, targetID)
	oldTS := scanCache[key]
	scanCache[key] = ts
	if ts != oldTS {
		dirtyScanKeys[key] = true
	}
	metadataMu.Unlock()

	logger.Debugf("[CACHE] Updated memory scan_ts for %s:%s -> %s (dirty: %v)", source, targetID, ts, ts != oldTS)
	return nil
}

func PersistScanMetadata(userEmail, source, targetID, ts string) error {
	query := `INSERT INTO scan_metadata (user_email, source, target_id, last_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (user_email, source, target_id)
		DO UPDATE SET last_ts = EXCLUDED.last_ts;`
	_, err := db.Exec(query, userEmail, source, targetID, ts)
	return err
}

func PersistAllScanMetadata(userEmail string) {
	metadataMu.RLock()
	var toPersist []struct{ source, target, ts string }
	prefix := userEmail + ":"
	for key := range dirtyScanKeys {
		if strings.HasPrefix(key, prefix) {
			parts := strings.Split(key, ":")
			if len(parts) == 3 {
				ts := scanCache[key]
				toPersist = append(toPersist, struct{ source, target, ts string }{parts[1], parts[2], ts})
			}
		}
	}
	metadataMu.RUnlock()

	if len(toPersist) == 0 {
		return // 변경된 내역이 없으면 DB 연결 시도조차 하지 않음 (Sleep 유지)
	}

	err := WithDBRetry("PersistAllScanMetadata", func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback() // 성공 시 Commit 되므로 지장 없음

		stmt, err := tx.Prepare(`INSERT INTO scan_metadata (user_email, source, target_id, last_ts)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (user_email, source, target_id)
			DO UPDATE SET last_ts = EXCLUDED.last_ts;`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, item := range toPersist {
			if _, err := stmt.Exec(userEmail, item.source, item.target, item.ts); err != nil {
				return err
			}
		}
		return tx.Commit()
	})

	if err != nil {
		logger.Errorf("Failed to persist scan metadata after retries: %v", err)
		return
	}

	metadataMu.Lock()
	for _, item := range toPersist {
		key := userEmail + ":" + item.source + ":" + item.target
		// 동시성 방어: DB에 쓰는 동안 새 업데이트가 발생하지 않았을 때만 dirty 플래그 해제
		if scanCache[key] == item.ts {
			delete(dirtyScanKeys, key)
		}
	}
	metadataMu.Unlock()
}

func FlushAllScanMetadata() {
	metadataMu.RLock()
	usersToFlush := make(map[string]bool)
	for key := range dirtyScanKeys {
		parts := strings.Split(key, ":")
		if len(parts) >= 1 {
			usersToFlush[parts[0]] = true
		}
	}
	metadataMu.RUnlock()

	for email := range usersToFlush {
		PersistAllScanMetadata(email)
	}
}

type SlackThreadMeta struct {
	UserEmail string
	ChannelID string
	ThreadTS  string
	LastTS    string
}

func RegisterActiveSlackThread(email, channelID, threadTS string) error {
	targetID := channelID + "|" + threadTS
	key := fmt.Sprintf("%s:slack_thread:%s", email, targetID)

	// DB를 깨우지 않고 지연 쓰기(Lazy Write)를 위해 메모리와 Dirty 캐시에만 등록
	metadataMu.Lock()
	if _, exists := scanCache[key]; !exists {
		scanCache[key] = threadTS
		dirtyScanKeys[key] = true
	}
	metadataMu.Unlock()
	return nil
}

func GetActiveSlackThreads() ([]SlackThreadMeta, error) {
	var threads []SlackThreadMeta
	// DB를 조회하지 않고 서버 구동 시 로드된 메모리 캐시(scanCache)에서 즉시 필터링
	metadataMu.RLock()
	for key, lastTS := range scanCache {
		parts := strings.Split(key, ":")
		if len(parts) == 3 && parts[1] == "slack_thread" {
			targetParts := strings.Split(parts[2], "|")
			if len(targetParts) == 2 {
				threads = append(threads, SlackThreadMeta{UserEmail: parts[0], ChannelID: targetParts[0], ThreadTS: targetParts[1], LastTS: lastTS})
			}
		}
	}
	metadataMu.RUnlock()
	return threads, nil
}

func UpdateSlackThreadLastTS(email, channelID, threadTS, lastTS string) error {
	// 즉시 DB에 쓰지 않고 기존 메인 스캐너의 지연 쓰기 파이프라인에 묻어서(Piggybacking) 처리
	return UpdateLastScan(email, "slack_thread", channelID+"|"+threadTS, lastTS)
}

func RemoveActiveSlackThread(email, channelID, threadTS string) error {
	targetID := channelID + "|" + threadTS
	key := fmt.Sprintf("%s:slack_thread:%s", email, targetID)

	metadataMu.Lock()
	delete(scanCache, key)
	delete(dirtyScanKeys, key)
	metadataMu.Unlock()

	_, err := db.Exec("DELETE FROM scan_metadata WHERE user_email = ? AND source = 'slack_thread' AND target_id = ?", email, targetID)
	return err
}
