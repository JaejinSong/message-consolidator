package handlers

import (
	"fmt"
	"math/rand"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"sync"
	"time"
)

var ScanFunc func(string, string)
var FullScanFunc func()
var (
	scanMutex  sync.Mutex
	isScanning bool
)

func (a *API) HandleManualScan(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "Korean"
	}
	logger.Debugf("Manual scan triggered via API for %s (lang: %s)", email, lang)

	if ScanFunc != nil {
		go func() {
			ScanFunc(email, lang)
			store.PersistAllScanMetadata(email)
			_ = services.FlushGamificationData() // Piggyback: Flush memory queue after manual scan to persist gamification data
		}()
	}

	w.WriteHeader(http.StatusOK)
}
func (a *API) HandleInternalScan(w http.ResponseWriter, r *http.Request) {
	if a.Config.InternalScanSecret == "" {
		logger.Warnf("[INTERNAL-SCAN] Internal scan attempt for unconfigured secret from %s", r.RemoteAddr)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	secret := r.Header.Get("X-Internal-Secret")
	if secret != a.Config.InternalScanSecret {
		logger.Warnf("[INTERNAL-SCAN] Unauthorized access attempt from %s", r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Use a mutex lock to prevent concurrent full scans, which could overwhelm the database and API limits
	scanMutex.Lock()
	if isScanning {
		scanMutex.Unlock()
		logger.Warnf("[INTERNAL-SCAN] Full scan skipped: already in progress")
		respondJSON(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "scan already in progress"})
		return
	}
	isScanning = true
	scanMutex.Unlock()

	defer func() {
		scanMutex.Lock()
		isScanning = false
		scanMutex.Unlock()
	}()

	// Add a random jitter of 0-5 seconds only in Cloud Run mode to prevent thundering herd problem (resonance)
	if a.Config.CloudRunMode {
		jitter := time.Duration(rand.Intn(5)) * time.Second
		logger.Debugf("[INTERNAL-SCAN] Cloud Run Mode: Sleeping for %v jitter...", jitter)
		time.Sleep(jitter)
	}

	logger.Infof("[INTERNAL-SCAN] Starting full scan triggered via API")
	if FullScanFunc != nil {
		FullScanFunc()
	} else {
		logger.Errorf("[INTERNAL-SCAN] FullScanFunc not initialized")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Piggyback: Flush accumulated gamification data from memory while the DB is active during the regular scan
	if err := services.FlushGamificationData(); err != nil {
		logger.Errorf("[INTERNAL-SCAN] Gamification flush error: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Scan completed successfully")
}

func (a *API) HandleTranslate(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		respondJSON(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "empty language"})
		return
	}

	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Perform a deep copy to prevent contamination of the cached array
	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	filter := store.ArchiveFilter{
		Email: email,
		Limit: 200,
	}
	archived, _, err := store.GetArchivedMessagesFiltered(r.Context(), filter)
	if err == nil {
		msgs = append(msgs, archived...)
	}

	// Extract a unique set of IDs to prevent duplicate translation requests and reduce API costs
	uniqueIDs := make(map[int]bool)
	var idList []int
	for _, m := range msgs {
		if !uniqueIDs[m.ID] {
			uniqueIDs[m.ID] = true
			idList = append(idList, m.ID)
		}
	}

	existingTranslations, err := store.GetTaskTranslationsBatch(idList, lang)
	if err != nil {
		logger.Errorf("[TRANSLATE] DB query failed for existing translations: %v", err)
		// Prevent token explosion by blocking translation if the DB query fails, as a nil map would cause all items to be re-translated
		http.Error(w, "Failed to check existing translations", http.StatusInternalServerError)
		return
	}

	var toTranslateIDs []int
	for _, id := range idList {
		if _, ok := existingTranslations[id]; !ok {
			toTranslateIDs = append(toTranslateIDs, id)
		}
	}

	logger.Infof("[TRANSLATE] Found %d messages needing translation to %s for %s", len(toTranslateIDs), lang, email)

	if len(toTranslateIDs) > 0 {
		const batchSize = 30 // Maximum number of items to send to the AI at once to prevent token explosion and response truncation
		var totalTranslated int

		for i := 0; i < len(toTranslateIDs); i += batchSize {
			end := i + batchSize
			if end > len(toTranslateIDs) {
				end = len(toTranslateIDs)
			}
			chunk := toTranslateIDs[i:end]

			count, err := a.TranslateMessagesByID(r.Context(), email, chunk, lang)
			if err != nil {
				logger.Errorf("[TRANSLATE] Partial translation failed at chunk %d: %v", i, err)
				break // Stop on error, but retain successfully translated items up to this point in the DB/cache
			}
			totalTranslated += count
		}
		logger.Infof("[TRANSLATE] Successfully translated %d/%d messages to %s", totalTranslated, len(toTranslateIDs), lang)
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":           "success",
		"translated_count": fmt.Sprintf("%d", len(toTranslateIDs)),
	})
}

func (a *API) HandleReclassifyOldData(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliases, _ := store.GetUserAliases(user.ID)

	msgs, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fixedCount := services.ReclassifyUserTasks(email, user, aliases, msgs)
	respondJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}

func (a *API) HandleRestoreGmailCC(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)

	svc, err := channels.GetGmailService(r.Context(), email)
	if err != nil {
		http.Error(w, "Gmail service error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	user, _ := store.GetOrCreateUser(email, "", "")
	aliases, _ := store.GetUserAliases(user.ID)

	activeMsgs, _ := store.GetMessages(email)
	filter := store.ArchiveFilter{
		Email: email,
		Limit: 10000,
	}
	archivedMsgs, _, _ := store.GetArchivedMessagesFiltered(r.Context(), filter)

	var allMsgs []store.ConsolidatedMessage
	allMsgs = append(allMsgs, activeMsgs...)
	allMsgs = append(allMsgs, archivedMsgs...)

	fixedCount := services.RestoreGmailCCAssignment(r.Context(), email, user, aliases, allMsgs, svc)
	respondJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}
