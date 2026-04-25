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
	
	// lastArchiveTime tracks the last successful auto-archive execution to ensure throttled processing.
	lastArchiveTime  time.Time
	
	// messageCache provides a fast lookup for active tasks in a user's dashboard.
	messageCache     = make(map[string][]ConsolidatedMessage)
	
	// archiveCache provides a fast lookup for completed or dismissed tasks.
	archiveCache     = make(map[string][]ConsolidatedMessage)
	
	// knownTS maintains a registry of processed message timestamps to eliminate duplicate entries during synchronization.
	knownTS          = make(map[string]map[string]bool)
	
	// cacheInitialized track whether a specific user's active message cache has been populated.
	cacheInitialized = make(map[string]bool)

	// archiveInitialized tracks whether the archive cache has been separately populated.
	archiveInitialized = make(map[string]bool)

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
	GlobalContactDSU.Reset()

	archiveMu.Lock()
	lastArchiveTime = time.Time{}
	archiveMu.Unlock()

	cacheMu.Lock()
	messageCache = make(map[string][]ConsolidatedMessage)
	archiveCache = make(map[string][]ConsolidatedMessage)
	knownTS = make(map[string]map[string]bool)
	cacheInitialized = make(map[string]bool)
	archiveInitialized = make(map[string]bool)
	cacheMu.Unlock()

	// Why: tests recreate the in-memory DB between cases, so the once-guard must reset
	// to allow the contact_resolution backfill to re-run against the fresh schema.
	migrateContactResolutionOnce = sync.Once{}
}


func RefreshAllCaches(ctx context.Context) error {
	users, err := GetAllUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := RefreshCache(ctx, u.Email); err != nil {
			logger.Errorf("Failed to refresh active cache for %s: %v", u.Email, err)
		}
		if err := RefreshArchiveCache(ctx, u.Email); err != nil {
			logger.Errorf("Failed to refresh archive cache for %s: %v", u.Email, err)
		}
	}
	return nil
}

func buildMessages(rows []db.RefreshCacheActiveRow, resolver map[string]ResolvedContact, knownTS map[string]bool) []ConsolidatedMessage {
	msgs := make([]ConsolidatedMessage, 0, len(rows))
	for _, r := range rows {
		reqDisplay, reqCanon, reqType := resolveContact(resolver, r.Requester)
		asgDisplay, asgCanon, asgType := resolveContact(resolver, r.Assignee)
		m := MapVMessageToConsolidated(
			MessageID(r.ID), r.UserEmail, r.Source, r.Room, r.Task,
			reqDisplay, asgDisplay, r.Link, r.SourceTs,
			r.OriginalText, r.Done.Bool, r.IsDeleted.Bool, r.CreatedAt,
			r.Category, r.Deadline, r.ThreadID,
			reqCanon, asgCanon, r.AssigneeReason,
			r.RepliedToID, int(r.IsContextQuery.Int64), r.Constraints,
			r.ConsolidatedContext, r.Metadata, r.SourceChannels,
			reqType, asgType, r.Subtasks,
			r.AssignedAt, r.CompletedAt,
		)
		msgs = append(msgs, m)
		knownTS[m.SourceTS] = true
	}
	return msgs
}

func buildArchiveMessages(rows []db.RefreshCacheArchiveRow, resolver map[string]ResolvedContact) []ConsolidatedMessage {
	msgs := make([]ConsolidatedMessage, 0, len(rows))
	for _, r := range rows {
		reqDisplay, reqCanon, reqType := resolveContact(resolver, r.Requester)
		asgDisplay, asgCanon, asgType := resolveContact(resolver, r.Assignee)
		msgs = append(msgs, MapVMessageToConsolidated(
			MessageID(r.ID), r.UserEmail, r.Source, r.Room, r.Task,
			reqDisplay, asgDisplay, r.Link, r.SourceTs,
			r.OriginalText, r.Done.Bool, r.IsDeleted.Bool, r.CreatedAt,
			r.Category, r.Deadline, r.ThreadID,
			reqCanon, asgCanon, r.AssigneeReason,
			r.RepliedToID, int(r.IsContextQuery.Int64), r.Constraints,
			r.ConsolidatedContext, r.Metadata, r.SourceChannels,
			reqType, asgType, r.Subtasks,
			r.AssignedAt, r.CompletedAt,
		))
	}
	return msgs
}

// RefreshCache reloads only the active message cache for a user.
// Contact resolution is performed in Go to avoid expensive view JOINs.
func RefreshCache(ctx context.Context, email string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resolver, err := BuildContactResolver(ctx, email)
	if err != nil {
		return fmt.Errorf("contact resolver: %w", err)
	}

	rows, err := db.New(GetDB()).RefreshCacheActive(ctx, nullString(email))
	if err != nil {
		return fmt.Errorf("active query failed: %w", err)
	}

	newKnownTS := make(map[string]bool)
	newActive := buildMessages(rows, resolver, newKnownTS)

	cacheMu.Lock()
	messageCache[email] = newActive
	knownTS[email] = newKnownTS
	cacheInitialized[email] = true
	cacheMu.Unlock()
	return nil
}

// RefreshArchiveCache reloads only the archive cache for a user.
// Separated from RefreshCache so the active path avoids the archive query on cold start.
func RefreshArchiveCache(ctx context.Context, email string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resolver, err := BuildContactResolver(ctx, email)
	if err != nil {
		return fmt.Errorf("contact resolver: %w", err)
	}

	rows, err := db.New(GetDB()).RefreshCacheArchive(ctx, nullString(email))
	if err != nil {
		return fmt.Errorf("archive query failed: %w", err)
	}

	newArchive := buildArchiveMessages(rows, resolver)

	cacheMu.Lock()
	archiveCache[email] = newArchive
	archiveInitialized[email] = true
	cacheMu.Unlock()
	return nil
}

// ensureCache is the shared singleflight-guarded initializer for both cache kinds.
func ensureCache(sfKey string, isReady func() bool, refresh func() error) error {
	cacheMu.RLock()
	ready := isReady()
	cacheMu.RUnlock()
	if ready {
		return nil
	}
	_, err, _ := sfGroup.Do(sfKey, func() (interface{}, error) {
		return nil, refresh()
	})
	return err
}

func EnsureCacheInitialized(ctx context.Context, email string) error {
	return ensureCache(email,
		func() bool { return cacheInitialized[email] },
		func() error { return RefreshCache(ctx, email) },
	)
}

func EnsureArchiveCacheInitialized(ctx context.Context, email string) error {
	return ensureCache("archive:"+email,
		func() bool { return archiveInitialized[email] },
		func() error { return RefreshArchiveCache(ctx, email) },
	)
}

// InvalidateCacheActive clears only the active message cache.
// Use this when a write affects only active (non-archived) messages.
func InvalidateCacheActive(email string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	delete(messageCache, email)
	delete(knownTS, email)
	delete(cacheInitialized, email)
}

// InvalidateCache clears both active and archive caches.
// Use this when a write may affect archived messages (delete, hard-delete, restore).
func InvalidateCache(email string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	delete(messageCache, email)
	delete(archiveCache, email)
	delete(knownTS, email)
	delete(cacheInitialized, email)
	delete(archiveInitialized, email)
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
