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
	knownTS          = make(map[string]map[string]bool) // Map: user_email -> source_ts -> exists
	cacheInitialized = make(map[string]bool)
	cacheMu          sync.RWMutex

	// Memory Caches for Metadata (avoids DB hits during idle scans)
	userCache        = make(map[string]*User)
	aliasCache       = make(map[int][]string)
	scanCache        = make(map[string]string)
	dirtyScanKeys    = make(map[string]bool) // Tracks modified scan timestamps not yet persisted to DB
	tokenCache       = make(map[string]string)
	tenantAliasCache = make(map[string]map[string]string) // Map: tenant_email -> original_name -> primary_name
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
