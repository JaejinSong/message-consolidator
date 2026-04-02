package store

import (
	"context"
	"fmt"
	"message-consolidator/logger"
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
)

func ResetForTest() {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	userCache = make(map[string]*User)
	scanCache = make(map[string]string)
	dirtyScanKeys = make(map[string]bool)
	tokenCache = make(map[string]string)
	contactsCache = make(map[string][]ContactRecord)

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

func RefreshAllCaches() error {
	users, err := GetAllUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := RefreshCache(u.Email); err != nil {
			logger.Errorf("Failed to refresh cache for %s: %v", u.Email, err)
		}
	}
	return nil
}

func RefreshCache(email string) error {
	//Why: Prevents cache refresh operations from hanging indefinitely by enforcing a 10-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	safeArchiveDays := GetAutoArchiveDays()
	threshold := fmt.Sprintf("-%d days", safeArchiveDays)

	//Why: Retrieves recently active messages to populate the primary cache.
	rows, err := db.QueryContext(ctx, SQL.RefreshCacheActive, email, threshold)
	if err != nil {
		return err
	}
	defer rows.Close()

	var newActive = []ConsolidatedMessage{}
	newKnownTS := make(map[string]bool)
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return err
		}
		newActive = append(newActive, m)
		newKnownTS[m.SourceTS] = true
	}

	//Why: Retrieves recently archived messages to populate the secondary cache.
	rowsArch, err := db.QueryContext(ctx, SQL.RefreshCacheArchive, email, threshold)
	if err != nil {
		return err
	}
	defer rowsArch.Close()

	var newArchive = []ConsolidatedMessage{}
	for rowsArch.Next() {
		m, err := scanMessageRow(rowsArch)
		if err != nil {
			return err
		}
		newArchive = append(newArchive, m)
		newKnownTS[m.SourceTS] = true
	}

	cacheMu.Lock()
	messageCache[email] = newActive
	archiveCache[email] = newArchive
	knownTS[email] = newKnownTS
	cacheInitialized[email] = true
	cacheMu.Unlock()

	return nil
}

func EnsureCacheInitialized(email string) error {
	cacheMu.RLock()
	initialized := cacheInitialized[email]
	cacheMu.RUnlock()

	if !initialized {
		return RefreshCache(email)
	}
	return nil
}

func ArchiveOldTasks() error {
	archiveMu.Lock()
	defer archiveMu.Unlock()

	//Why: Throttles background archiving to once every six hours to optimize resource usage.
	if time.Since(lastArchiveTime) < 6*time.Hour {
		return nil
	}

	//Why: Limits archiving task duration to 15 seconds to prevent database performance degradation or locks.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
		_ = RefreshAllCaches()
	}
	return nil
}
