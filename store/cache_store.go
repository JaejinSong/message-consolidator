package store

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"golang.org/x/sync/singleflight"
	"sync"
	"time"
)

var (
	metadataMu sync.RWMutex
	archiveMu  sync.RWMutex
	cacheMu    sync.RWMutex

	// userCache maps email addresses to User objects for rapid profile and preference lookups.
	userCache        = make(map[string]*User)
	
	// scanCache stores the last processed timestamp for each source to prevent redundant processing of historical data.
	scanCache        = make(map[string]string)
	dirtyScanKeys    = make(map[string]bool)
	
	// tokenCache holds OAuth refresh tokens for background service authentications.
	tokenCache       = make(map[string]string)
	
	// contactsCache stores consolidated identity mappings (SSOT) to improve requester identification across platforms.
	contactsCache    = make(map[string][]ContactRecord)

	// aliasCache stores message aliases for a specific contact (Identity-X resolution support).
	aliasCache       = make(map[int64][]string)

	// lastArchiveTime tracks the last successful auto-archive execution to ensure throttled processing.
	lastArchiveTime  time.Time
	
	// messageCache provides a fast lookup for active tasks in a user's dashboard.
	messageCache     = make(map[string][]ConsolidatedMessage)
	
	// archiveCache provides a fast lookup for completed or dismissed tasks.
	archiveCache     = make(map[string][]ConsolidatedMessage)
	
	// knownTS maintains a registry of processed message timestamps to eliminate duplicate entries during synchronization.
	knownTS          = make(map[string]map[string]bool)
	
	// cacheInitialized track whether a specific user's message cache has been populated.
	cacheInitialized = make(map[string]bool)

	// sfGroup handles single-flight requests to prevent cache stampede.
	sfGroup singleflight.Group
)

func ResetForTest() {
	if db != nil {
		tables := []string{
			"users", "user_aliases", "gmail_tokens", "messages", "task_translations",
			"tenant_aliases", "scan_metadata", "user_achievements",
			"contacts", "reports", "report_translations", "prompt_logs",
			"ai_inference_logs", "contact_aliases", "identity_merge_history",
			"identity_merge_candidates", "slack_threads",
		}
		for _, table := range tables {
			_, _ = db.Exec("DELETE FROM " + table)
		}
	}

	metadataMu.Lock()
	defer metadataMu.Unlock()
	userCache = make(map[string]*User)
	scanCache = make(map[string]string)
	dirtyScanKeys = make(map[string]bool)
	tokenCache = make(map[string]string)
	contactsCache = make(map[string][]ContactRecord)
	aliasCache = make(map[int64][]string)
	GlobalContactDSU.Reset()

	archiveMu.Lock()
	lastArchiveTime = time.Time{}
	archiveMu.Unlock()

	cacheMu.Lock()
	messageCache = make(map[string][]ConsolidatedMessage)
	archiveCache = make(map[string][]ConsolidatedMessage)
	knownTS = make(map[string]map[string]bool)
	cacheInitialized = make(map[string]bool)
	cacheMu.Unlock()
}

func GetContactsCache() map[string][]ContactRecord {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	return contactsCache
}

func RefreshAllCaches(ctx context.Context) error {
	users, err := GetAllUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := RefreshCache(ctx, u.Email); err != nil {
			logger.Errorf("Failed to refresh cache for %s: %v", u.Email, err)
		}
	}
	return nil
}

func fetchCacheActive(ctx context.Context, email, threshold string, knownTS map[string]bool) ([]ConsolidatedMessage, error) {
	rows, err := db.QueryContext(ctx, SQL.RefreshCacheActive, email, threshold)
	if err != nil {
		return nil, fmt.Errorf("active query failed: %w", err)
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, fmt.Errorf("active scan failed: %w", err)
		}
		msgs = append(msgs, m)
		knownTS[m.SourceTS] = true
	}
	return msgs, nil
}

func fetchCacheArchive(ctx context.Context, email, threshold string, knownTS map[string]bool) ([]ConsolidatedMessage, error) {
	rows, err := db.QueryContext(ctx, SQL.RefreshCacheArchive, email, threshold)
	if err != nil {
		return nil, fmt.Errorf("archive query failed: %w", err)
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, fmt.Errorf("archive scan failed: %w", err)
		}
		msgs = append(msgs, m)
		knownTS[m.SourceTS] = true
	}
	return msgs, nil
}

func RefreshCache(ctx context.Context, email string) error {
	//Why: Prevents cache refresh operations from hanging indefinitely by enforcing a 10-second timeout.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	threshold := fmt.Sprintf("-%d days", GetAutoArchiveDays())
	newKnownTS := make(map[string]bool)

	// [Modular logic] Split active and archive fetching to keep functional complexity low (under 40 lines).
	newActive, err := fetchCacheActive(ctx, email, threshold, newKnownTS)
	if err != nil {
		return err
	}

	newArchive, err := fetchCacheArchive(ctx, email, threshold, newKnownTS)
	if err != nil {
		return err
	}

	cacheMu.Lock()
	messageCache[email] = newActive
	archiveCache[email] = newArchive
	knownTS[email] = newKnownTS
	cacheInitialized[email] = true
	cacheMu.Unlock()

	return nil
}

func EnsureCacheInitialized(ctx context.Context, email string) error {
	cacheMu.RLock()
	initialized := cacheInitialized[email]
	cacheMu.RUnlock()

	if initialized {
		return nil
	}
	// Why: Use singleflight to prevent multiple concurrent DB hits for the same user.
	_, err, _ := sfGroup.Do(email, func() (interface{}, error) {
		return nil, RefreshCache(ctx, email)
	})
	return err
}

func InvalidateCache(email string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	delete(messageCache, email)
	delete(archiveCache, email)
	delete(knownTS, email)
	delete(cacheInitialized, email)
}

func ArchiveOldTasks(ctx context.Context) error {
	archiveMu.Lock()
	defer archiveMu.Unlock()

	//Why: Throttles background archiving to once every six hours to optimize resource usage.
	if time.Since(lastArchiveTime) < 6*time.Hour {
		return nil
	}

	//Why: Limits archiving task duration to 15 seconds to prevent database performance degradation or locks.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	safeArchiveDays := GetAutoArchiveDays()
	threshold := fmt.Sprintf("-%d days", safeArchiveDays)

	logger.Infof("[DB] Auto-archiving tasks completed more than %d days ago...", safeArchiveDays)
	res, err := db.ExecContext(ctx, SQL.ArchiveOldTasks, threshold)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	logger.Infof("[DB] Auto-archived %d tasks.", rows)

	lastArchiveTime = time.Now()

	if rows > 0 {
		_ = RefreshAllCaches(ctx)
	}
	return nil
}
// ClearAliasCache removes the cached aliases for a specific contact.
func ClearAliasCache(contactID int64) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	delete(aliasCache, contactID)
}

// GetAliasesForContact retrieves aliases for a contact from cache or DB (Cache-Aside).
func GetAliasesForContact(ctx context.Context, contactID int64) ([]string, error) {
	if cached := getCachedAliases(contactID); cached != nil {
		return cached, nil
	}

	key := fmt.Sprintf("aliases:%d", contactID)
	val, err, _ := sfGroup.Do(key, func() (interface{}, error) {
		return fetchAndCacheAliases(ctx, contactID)
	})

	if err != nil {
		return nil, err
	}
	return val.([]string), nil
}

func getCachedAliases(id int64) []string {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	return aliasCache[id]
}

func fetchAndCacheAliases(ctx context.Context, id int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, SQL.GetContactAliases, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []string
	for rows.Next() {
		var a struct {
			ID             int64
			ContactID      int64
			Type           string
			Value          string
			Source         string
			Trust          int
			CreatedAt      time.Time
		}
		if err := rows.Scan(&a.ID, &a.ContactID, &a.Type, &a.Value, &a.Source, &a.Trust, &a.CreatedAt); err == nil {
			aliases = append(aliases, a.Value)
		}
	}

	metadataMu.Lock()
	defer metadataMu.Unlock()
	aliasCache[id] = aliases
	return aliases, nil
}
