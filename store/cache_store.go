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
	knownTS          = make(map[string]map[string]bool) // user_email -> source_ts -> bool
	cacheInitialized = make(map[string]bool)
	cacheMu          sync.RWMutex

	// Memory Caches for Metadata (to avoid DB hits during idle scans)
	userCache        = make(map[string]*User)
	aliasCache       = make(map[int][]string)
	scanCache        = make(map[string]string)            // key: email:source:targetID -> lastTS
	dirtyScanKeys    = make(map[string]bool)              // DB에 아직 persist 안 된 변경된 scanTS 목록
	tokenCache       = make(map[string]string)            // email -> gmail token json
	tenantAliasCache = make(map[string]map[string]string) // tenant_email -> original_name -> primary_name
	contactsCache    = make(map[string][]AliasMapping)    // email -> mappings
	metadataMu       sync.RWMutex
	lastArchiveTime  time.Time
	archiveMu        sync.Mutex
	autoArchiveDays  int = 1
)

func SetAutoArchiveDays(days int) {
	autoArchiveDays = days
}
