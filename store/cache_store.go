package store

import (
	"database/sql"
	"sync"
	"time"
)

var (
	db               *sql.DB
	messageCache     = make(map[string][]ConsolidatedMessage)
	archiveCache     = make(map[string][]ConsolidatedMessage)
	knownTS          = make(map[string]map[string]bool) //Why: Tracks known message timestamps per user to prevent duplicate processing.
	cacheInitialized = make(map[string]bool)
	cacheMu          sync.RWMutex

	//Why: Maintains in-memory caches for frequently accessed metadata to minimize database load during passive operations.
	userCache        = make(map[string]*User)
	aliasCache       = make(map[int][]string)
	scanCache        = make(map[string]string)
	dirtyScanKeys    = make(map[string]bool) //Why: Identifies scan timestamps that have changed locally and need to be synchronized with the database.
	tokenCache       = make(map[string]string)
	tenantAliasCache = make(map[string]map[string]string) //Why: Maps localized or variation names to standard primary names within a specific tenant context.
	contactsCache    = make(map[string][]AliasMapping)
	metadataMu       sync.RWMutex
	lastArchiveTime  time.Time
	archiveMu        sync.Mutex
)

func ResetForTest() {
	cacheMu.Lock()
	messageCache = make(map[string][]ConsolidatedMessage)
	archiveCache = make(map[string][]ConsolidatedMessage)
	knownTS = make(map[string]map[string]bool)
	cacheInitialized = make(map[string]bool)
	cacheMu.Unlock()

	metadataMu.Lock()
	userCache = make(map[string]*User)
	aliasCache = make(map[int][]string)
	scanCache = make(map[string]string)
	dirtyScanKeys = make(map[string]bool)
	tokenCache = make(map[string]string)
	tenantAliasCache = make(map[string]map[string]string)
	contactsCache = make(map[string][]AliasMapping)
	metadataMu.Unlock()

	archiveMu.Lock()
	lastArchiveTime = time.Time{}
	archiveMu.Unlock()
}
