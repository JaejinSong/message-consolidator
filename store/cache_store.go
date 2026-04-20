package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
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
	// Why: testMode skips expensive RefreshAllCaches during schema re-init.
	testMode = true

	// Why: For in-memory SQLite, closing the connection destroys the database entirely.
	// This is simpler and more reliable than DROP TABLE ... for each table.
	// A unique DSN ensures the next InitDB creates a completely fresh database.
	if conn != nil {
		conn.Close()
		conn = nil
	}
	dsn = ""
	// Why: A unique name per reset prevents any cross-test state bleed via SQLite's
	// internal URI-keyed database registry.
	TestDSN = fmt.Sprintf("file:memdb_%d?mode=memory&cache=shared", time.Now().UnixNano())

	metadataMu.Lock()
	defer metadataMu.Unlock()
	userCache = make(map[string]*User)
	scanCache = make(map[string]string)
	dirtyScanKeys = make(map[string]bool)
	tokenCache = make(map[string]string)
	contactsCache = make(map[string][]ContactRecord)
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

type cacheRowConvertible interface {
	db.RefreshCacheActiveRow | db.RefreshCacheArchiveRow
}

func collectCacheRows[T cacheRowConvertible](rows []T, knownTS map[string]bool, convert func(T) ConsolidatedMessage) []ConsolidatedMessage {
	var msgs []ConsolidatedMessage
	for _, r := range rows {
		m := convert(r)
		msgs = append(msgs, m)
		knownTS[m.SourceTS] = true
	}
	return msgs
}

func fetchCacheActive(ctx context.Context, email string, knownTS map[string]bool) ([]ConsolidatedMessage, error) {
	rows, err := db.New(GetDB()).RefreshCacheActive(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("active query failed: %w", err)
	}
	return collectCacheRows(rows, knownTS, func(r db.RefreshCacheActiveRow) ConsolidatedMessage {
		return MapVMessageToConsolidated(
			int(r.ID), r.UserEmail, r.Source, r.Room, r.Task,
			r.Requester, r.Assignee, r.Link, r.SourceTs,
			r.OriginalText, r.Done, r.IsDeleted, r.CreatedAt,
			r.Category, r.Deadline, r.ThreadID,
			r.RequesterCanonical, r.AssigneeCanonical, r.AssigneeReason,
			r.RepliedToID, int(r.IsContextQuery), r.Constraints,
			r.ConsolidatedContext, r.Metadata, r.SourceChannels,
			r.RequesterType, r.AssigneeType, r.Subtasks,
			r.AssignedAt, r.CompletedAt,
		)
	}), nil
}

func fetchCacheArchive(ctx context.Context, email string, knownTS map[string]bool) ([]ConsolidatedMessage, error) {
	rows, err := db.New(GetDB()).RefreshCacheArchive(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("archive query failed: %w", err)
	}
	return collectCacheRows(rows, knownTS, func(r db.RefreshCacheArchiveRow) ConsolidatedMessage {
		return MapVMessageToConsolidated(
			int(r.ID), r.UserEmail, r.Source, r.Room, r.Task,
			r.Requester, r.Assignee, r.Link, r.SourceTs,
			r.OriginalText, r.Done, r.IsDeleted, r.CreatedAt,
			r.Category, r.Deadline, r.ThreadID,
			r.RequesterCanonical, r.AssigneeCanonical, r.AssigneeReason,
			r.RepliedToID, int(r.IsContextQuery), r.Constraints,
			r.ConsolidatedContext, r.Metadata, r.SourceChannels,
			r.RequesterType, r.AssigneeType, r.Subtasks,
			r.AssignedAt, r.CompletedAt,
		)
	}), nil
}

func RefreshCache(ctx context.Context, email string) error {
	//Why: Prevents cache refresh operations from hanging indefinitely by enforcing a 10-second timeout.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	newKnownTS := make(map[string]bool)

	// [Modular logic] Split active and archive fetching to keep functional complexity low (under 40 lines).
	newActive, err := fetchCacheActive(ctx, email, newKnownTS)
	if err != nil {
		return err
	}

	newArchive, err := fetchCacheArchive(ctx, email, newKnownTS)
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
	queries := db.New(GetDB())
	rows, err := queries.ArchiveOldTasks(ctx, threshold)
	if err != nil {
		return err
	}
	logger.Infof("[DB] Auto-archiving tasks completed more than %d days ago triggered. Rows: %d", safeArchiveDays, rows)

	lastArchiveTime = time.Now()

	if rows > 0 {
		_ = RefreshAllCaches(ctx)
	}
	return nil
}
