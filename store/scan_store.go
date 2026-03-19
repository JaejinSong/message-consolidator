package store

import (
	"fmt"
	"message-consolidator/logger"
	"strings"
)

func LoadMetadata() error {
	metadataMu.Lock()
	defer metadataMu.Unlock()

	logger.Infof("[CACHE] Initializing metadata cache from DB...")

	// 1. Load Users
	rows, err := db.Query("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at FROM users")
	if err != nil {
		return fmt.Errorf("failed to load users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt); err != nil {
			return err
		}
		userCache[u.Email] = &u
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
			continue
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
		if err := tokenRows.Scan(&email, &token); err == nil {
			tokenCache[email] = token
		}
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

	logger.Infof("[CACHE] Loaded %d users, %d scan entries, %d tokens, %d tenant aliases.", len(userCache), len(scanCache), len(tokenCache), len(tenantAliasCache))
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
		VALUES ($1, $2, $3, $4)
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

	tx, err := db.Begin()
	if err != nil {
		logger.Errorf("Failed to begin tx for scan metadata: %v", err)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO scan_metadata (user_email, source, target_id, last_ts)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_email, source, target_id)
		DO UPDATE SET last_ts = EXCLUDED.last_ts;`)
	if err != nil {
		logger.Errorf("Failed to prepare stmt for scan metadata: %v", err)
		return
	}
	defer stmt.Close()

	for _, item := range toPersist {
		if _, err := stmt.Exec(userEmail, item.source, item.target, item.ts); err != nil {
			logger.Errorf("Failed to exec stmt for scan metadata: %v", err)
			return // 에러 발생 시 rollback
		}
	}
	_ = tx.Commit()

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
