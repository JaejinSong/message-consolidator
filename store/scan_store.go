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

	// 5. Load Tenant Aliases
	aliasRows, err = db.Query("SELECT user_email, original_name, primary_name FROM tenant_aliases")
	if err != nil {
		return fmt.Errorf("failed to load tenant aliases: %w", err)
	}
	defer aliasRows.Close()

	for aliasRows.Next() {
		var email, original, primary string
		if err := aliasRows.Scan(&email, &original, &primary); err != nil {
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
	for key, ts := range scanCache {
		if strings.HasPrefix(key, prefix) && dirtyScanKeys[key] {
			parts := strings.Split(key, ":")
			if len(parts) == 3 {
				toPersist = append(toPersist, struct{ source, target, ts string }{parts[1], parts[2], ts})
			}
		}
	}
	metadataMu.RUnlock()

	for _, item := range toPersist {
		_ = PersistScanMetadata(userEmail, item.source, item.target, item.ts)
		metadataMu.Lock()
		delete(dirtyScanKeys, userEmail+":"+item.source+":"+item.target)
		metadataMu.Unlock()
	}
}
