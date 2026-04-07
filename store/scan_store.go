package store

import (
	"database/sql"
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

	//Why: Loads user definitions from the database, ensuring names are trimmed for consistent mapping.
	rows, err := db.Query(SQL.LoadUsersSimple)
	if err != nil {
		return fmt.Errorf("failed to load users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u User
		var slackID, waJID sql.NullString
		var createdAt DBTime
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &slackID, &waJID, &u.Picture, &createdAt); err != nil {
			return fmt.Errorf("scan user failed: %w", err)
		}
		u.SlackID = slackID.String
		u.WAJID = waJID.String
		u.CreatedAt = createdAt.Time
		userCache[u.Email] = &u
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error in users: %w", err)
	}

	//Why: Restores the last scan timestamps for each source to memory for efficient duplicate detection.
	logger.Infof("[SCAN] Loading existing scan metadata into memory...")
	scanRows, err := db.Query(SQL.LoadScanMetadataAll)
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

	//Why: Loads OAuth refresh tokens into the cache to support background Gmail synchronization.
	logger.Infof("[SCAN] Loading existing gmail tokens into memory...")
	tokenRows, err := db.Query(SQL.LoadGmailTokensAll)
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

	//Why: Loads consolidated contact mappings for improved requester identification.
	logger.Infof("[CACHE] Loading consolidated contacts into memory...")
	contactRows, err := db.Query(SQL.LoadContactsAll)
	if err == nil {
		defer contactRows.Close()
		for contactRows.Next() {
			var tEmail, canonical, display, source string
			if err := contactRows.Scan(&tEmail, &canonical, &display, &source); err == nil {
				contactsCache[tEmail] = append(contactsCache[tEmail], ContactRecord{
					CanonicalID: canonical,
					DisplayName: display,
					Source:      source,
				})
			}
		}
	}

	logger.Infof("[CACHE] Loaded %d users, %d scan entries, %d tokens, %d contact mappings.", len(userCache), len(scanCache), len(tokenCache), len(contactsCache))
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
	_, err := db.Exec(SQL.UpsertScanMetadata, userEmail, source, targetID, ts)
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
		return //Why: Avoids unnecessary database connections if no dirty metadata entries exist, preserving resources.
	}

	err := WithDBRetry("PersistAllScanMetadata", func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback() //Why: Uses a deferred rollback to ensure transaction safety; it is overridden by an explicit commit on success.

		stmt, err := tx.Prepare(SQL.UpsertScanMetadata)
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
		//Why: Implements a concurrency guard to only clear the dirty flag if no new updates occurred during the persistence process.
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
	UserEmail      string
	ChannelID      string
	ThreadTS       string
	LastTS         string
	LastActivityTS string
}

func RegisterActiveSlackThread(email, channelID, threadTS string) error {
	targetID := channelID + "|" + threadTS
	key := fmt.Sprintf("%s:slack_thread:%s", email, targetID)

	//Why: Registers the thread timestamp in memory and dirty caches only to support lazy writes without waking the database.
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
	//Why: Filters active threads directly from the memory cache to avoid expensive database queries.
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
	//Why: Piggybacks on the existing lazy write pipeline instead of writing directly to the database.
	return UpdateLastScan(email, "slack_thread", channelID+"|"+threadTS, lastTS)
}

func RemoveActiveSlackThread(email, channelID, threadTS string) error {
	targetID := channelID + "|" + threadTS
	key := fmt.Sprintf("%s:slack_thread:%s", email, targetID)

	metadataMu.Lock()
	delete(scanCache, key)
	delete(dirtyScanKeys, key)
	metadataMu.Unlock()

	_, err := db.Exec(SQL.DeleteScanMetadataSlackThread, email, "slack_thread", targetID)
	return err
}

//Why: Provides support functions for the targeted Slack thread scanner worker.
func RegisterTargetedSlackThread(channelID, threadTS, lastReplyTS, userEmail string) error {
	_, err := db.Exec(SQL.UpsertSlackThread, channelID, threadTS, lastReplyTS, lastReplyTS, "active", userEmail)
	return err
}

func GetTargetedActiveThreads() ([]SlackThreadMeta, error) {
	rows, err := db.Query(SQL.GetActiveSlackThreadsNew)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []SlackThreadMeta
	for rows.Next() {
		var t SlackThreadMeta
		if err := rows.Scan(&t.ChannelID, &t.ThreadTS, &t.LastTS, &t.LastActivityTS, &t.UserEmail); err != nil {
			return nil, err
		}
		threads = append(threads, t)
	}
	return threads, nil
}

func UpdateTargetedThread(channelID, threadTS, lastReplyTS, lastActivityTS, userEmail string) error {
	_, err := db.Exec(SQL.UpsertSlackThread, channelID, threadTS, lastReplyTS, lastActivityTS, "active", userEmail)
	return err
}

func CloseTargetedThread(channelID, threadTS, userEmail string) error {
	_, err := db.Exec(SQL.CloseSlackThread, channelID, threadTS, userEmail)
	return err
}
