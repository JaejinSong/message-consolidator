package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/logger"
	"strings"
	"time"
)

// WithDBRetry executes a database operation with exponential backoff,
// designed to gracefully handle serverless DB (NeonDB) cold starts.
func WithDBRetry(operationName string, fn func() error) error {
	var err error
	maxRetries := 5
	baseDelay := 100 * time.Millisecond

	for i := 1; i <= maxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}

		// Why: Only retry on transient database errors (like SQLITE_BUSY or connection reset).
		errStr := err.Error()
		isTransient := strings.Contains(errStr, "database is locked") || 
					  strings.Contains(errStr, "SQLITE_BUSY") || 
					  strings.Contains(errStr, "connection refused") ||
					  strings.Contains(errStr, "handshake failed")

		if !isTransient {
			return err
		}

		// Why: Use exponential backoff to handle concurrent lock contention or serverless cold starts.
		// Progression: 100ms, 200ms, 400ms, 800ms, 1600ms
		delay := baseDelay * (1 << (i - 1))
		logger.Warnf("[DB-RETRY] %s failed (attempt %d/%d). Retrying in %v... Err: %v", operationName, i, maxRetries, delay, err)
		time.Sleep(delay)
	}
	return err
}

func LoadMetadata() error {
	metadataMu.Lock()
	defer metadataMu.Unlock()

	logger.Infof("[CACHE] Initializing metadata cache from DB...")

	//Why: Loads user definitions from the database, ensuring names are trimmed for consistent mapping.
	conn := GetDB()
	queries := db.New(conn)
	userRows, err := queries.LoadUsersAll(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load users: %w", err)
	}

	for _, row := range userRows {
		u := User{
			ID:        int(row.ID),
			Email:     row.Email.String,
			Name:      row.Name.String,
			SlackID:   row.SlackID.String,
			WAJID:     row.WaJid.String,
			Picture:   row.Picture.String,
			CreatedAt: row.CreatedAt.Time,
		}
		userCache[u.Email] = &u
	}

	//Why: Restores the last scan timestamps for each source to memory for efficient duplicate detection.
	logger.Infof("[SCAN] Loading existing scan metadata into memory...")
	scanRows, err := queries.LoadScanMetadataAll(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load scan metadata: %w", err)
	}

	for _, row := range scanRows {
		key := fmt.Sprintf("%s:%s:%s", row.UserEmail, row.Source, row.TargetID)
		scanCache[key] = row.LastTs.String
	}
	logger.Infof("[SCAN] Loaded %d scan metadata entries.", len(scanCache))

	//Why: Loads OAuth refresh tokens into the cache to support background Gmail synchronization.
	logger.Infof("[SCAN] Loading existing gmail tokens into memory...")
	tokenRows, err := queries.LoadGmailTokensAll(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load gmail tokens: %w", err)
	}

	for _, row := range tokenRows {
		tokenCache[row.UserEmail] = row.TokenJson
	}

	logger.Infof("[CACHE] Loaded %d users, %d scan entries, %d tokens.", len(userCache), len(scanCache), len(tokenCache))
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
	conn := GetDB()
	queries := db.New(conn)
	return queries.UpsertScanMetadata(context.Background(), db.UpsertScanMetadataParams{
		UserEmail: userEmail,
		Source:    source,
		TargetID:  targetID,
		LastTs:    nullString(ts),
	})
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
		conn := GetDB()
		tx, err := conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback() //Why: Uses a deferred rollback to ensure transaction safety; it is overridden by an explicit commit on success.

		queries := db.New(tx)

		for _, item := range toPersist {
			if err := queries.UpsertScanMetadata(context.Background(), db.UpsertScanMetadataParams{
				UserEmail: userEmail,
				Source:    item.source,
				TargetID:  item.target,
				LastTs:    nullString(item.ts),
			}); err != nil {
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

	conn := GetDB()
	queries := db.New(conn)
	return queries.DeleteScanMetadataSlackThread(context.Background(), db.DeleteScanMetadataSlackThreadParams{
		UserEmail: email,
		TargetID:  targetID,
	})
}

//Why: Provides support functions for the targeted Slack thread scanner worker.
func RegisterTargetedSlackThread(ctx context.Context, channelID, threadTS, lastReplyTS, userEmail string) error {
	queries := db.New(GetDB())
	return queries.UpsertSlackThread(ctx, db.UpsertSlackThreadParams{
		ChannelID:      nullString(channelID),
		ThreadTs:       nullString(threadTS),
		LastReplyTs:    nullString(lastReplyTS),
		LastActivityTs: nullString(lastReplyTS),
		UserEmail:      nullString(userEmail),
	})
}

func GetTargetedActiveThreads(ctx context.Context) ([]SlackThreadMeta, error) {
	queries := db.New(GetDB())
	rows, err := queries.GetActiveSlackThreadsNew(ctx)
	if err != nil {
		return nil, err
	}

	var threads []SlackThreadMeta
	for _, r := range rows {
		threads = append(threads, SlackThreadMeta{
			ChannelID:      r.ChannelID.String,
			ThreadTS:       r.ThreadTs.String,
			LastTS:         r.LastReplyTs.String,
			LastActivityTS: r.LastActivityTs.String,
			UserEmail:      r.UserEmail.String,
		})
	}
	return threads, nil
}

func UpdateTargetedThread(ctx context.Context, channelID, threadTS, lastReplyTS, lastActivityTS, userEmail string) error {
	queries := db.New(GetDB())
	return queries.UpsertSlackThread(ctx, db.UpsertSlackThreadParams{
		ChannelID:      nullString(channelID),
		ThreadTs:       nullString(threadTS),
		LastReplyTs:    nullString(lastReplyTS),
		LastActivityTs: nullString(lastActivityTS),
		UserEmail:      nullString(userEmail),
	})
}

func CloseTargetedThread(ctx context.Context, channelID, threadTS, userEmail string) error {
	queries := db.New(GetDB())
	return queries.CloseSlackThread(ctx, db.CloseSlackThreadParams{
		ChannelID: nullString(channelID),
		ThreadTs:  nullString(threadTS),
		UserEmail: nullString(userEmail),
	})
}
