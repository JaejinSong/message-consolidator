package handlers

import (
	"context"
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
			//Why: Synchronizes accumulated gamification data from the memory queue to the database immediately after a manual scan.
			_ = services.FlushGamificationData()
		}()
	}

	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleInternalScan(w http.ResponseWriter, r *http.Request) {
	if !a.authorizeInternalScan(w, r) {
		return
	}

	//Why: Prevents concurrent full scans using a mutex lock to avoid overwhelming database resources and hitting API rate limits.
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

	a.applyCloudRunJitter()

	logger.Infof("[INTERNAL-SCAN] Starting full scan triggered via API")
	if FullScanFunc != nil {
		FullScanFunc()
	} else {
		logger.Errorf("[INTERNAL-SCAN] FullScanFunc not initialized")
		respondError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	//Why: Leverages the active database connection during a full scan to efficiently flush accumulated gamification data from memory.
	if err := services.FlushGamificationData(); err != nil {
		logger.Errorf("[INTERNAL-SCAN] Gamification flush error: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Scan completed successfully")
}

// Why: Isolates security checks to keep the main handler focused on business logic.
func (a *API) authorizeInternalScan(w http.ResponseWriter, r *http.Request) bool {
	if a.Config.InternalScanSecret == "" {
		logger.Warnf("[INTERNAL-SCAN] Internal scan attempt for unconfigured secret from %s", r.RemoteAddr)
		respondError(w, http.StatusForbidden, "Forbidden")
		return false
	}
	secret := r.Header.Get("X-Internal-Secret")
	if secret != a.Config.InternalScanSecret {
		logger.Warnf("[INTERNAL-SCAN] Unauthorized access attempt from %s", r.RemoteAddr)
		respondError(w, http.StatusUnauthorized, "Unauthorized")
		return false
	}
	return true
}

func (a *API) applyCloudRunJitter() {
	//Why: Adds a random jitter in Cloud Run mode to staggered incoming requests and prevent 'thundering herd' synchronization issues.
	if a.Config.CloudRunMode {
		jitter := time.Duration(rand.Intn(5)) * time.Second
		logger.Debugf("[INTERNAL-SCAN] Cloud Run Mode: Sleeping for %v jitter...", jitter)
		time.Sleep(jitter)
	}
}

func (a *API) HandleTranslate(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		respondJSON(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "empty language"})
		return
	}

	msgs, err := a.gatherMessagesForTranslation(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	toTranslateIDs, err := a.filterUntranslatedIDs(msgs, lang)
	if err != nil {
		logger.Errorf("[TRANSLATE] DB query failed for existing translations: %v", err)
		//Why: Fails the request if the existing translation check fails to prevent expensive and unnecessary re-translation of all tasks.
		respondError(w, http.StatusInternalServerError, "Failed to check existing translations")
		return
	}

	logger.Infof("[TRANSLATE] Found %d messages needing translation to %s for %s", len(toTranslateIDs), lang, email)

	if len(toTranslateIDs) > 0 {
		a.processTranslationBatches(r.Context(), email, toTranslateIDs, lang)
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":           "success",
		"translated_count": fmt.Sprintf("%d", len(toTranslateIDs)),
	})
}

// Why: Consolidates active and archived message retrieval to ensure comprehensive translation coverage across the user's entire task history.
func (a *API) gatherMessagesForTranslation(ctx context.Context, email string) ([]store.ConsolidatedMessage, error) {
	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		return nil, err
	}

	//Why: Performs a deep copy of retrieved messages to avoid unintended modifications to the globally cached message array.
	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	filter := store.ArchiveFilter{
		Email: email,
		Limit: 200,
	}
	archived, _, err := store.GetArchivedMessagesFiltered(ctx, filter)
	if err == nil {
		msgs = append(msgs, archived...)
	}
	return msgs, nil
}

// Why: Optimizes AI API usage by stripping duplicate task IDs and filtering out tasks that have already been translated.
func (a *API) filterUntranslatedIDs(msgs []store.ConsolidatedMessage, lang string) ([]int, error) {
	//Why: Deduplicates message IDs before processing to avoid redundant translation requests and minimize AI API expenses.
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
		return nil, err
	}

	var toTranslateIDs []int
	for _, id := range idList {
		if _, ok := existingTranslations[id]; !ok {
			toTranslateIDs = append(toTranslateIDs, id)
		}
	}
	return toTranslateIDs, nil
}

// Why: Chunks large translation requests into manageable batches to stay within token limits and handle partial failures gracefully.
func (a *API) processTranslationBatches(ctx context.Context, email string, toTranslateIDs []int, lang string) {
	const batchSize = 30 //Why: Limits translation batches to 30 items to maintain optimal token usage and prevent response truncation from the AI model.
	var totalTranslated int

	for i := 0; i < len(toTranslateIDs); i += batchSize {
		end := i + batchSize
		if end > len(toTranslateIDs) {
			end = len(toTranslateIDs)
		}
		chunk := toTranslateIDs[i:end]

		count, err := a.TranslateMessagesByID(ctx, email, chunk, lang)
		if err != nil {
			logger.Errorf("[TRANSLATE] Partial translation failed at chunk %d: %v", i, err)
			break //Why: Terminates the translation loop on error while ensuring previously successful results are already persisted.
		}
		totalTranslated += count
	}
	logger.Infof("[TRANSLATE] Successfully translated %d/%d messages to %s", totalTranslated, len(toTranslateIDs), lang)
}

func (a *API) HandleReclassifyOldData(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	aliases, _ := store.GetUserAliases(user.ID)

	msgs, err := store.GetMessages(email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if a.Tasks == nil {
		respondError(w, http.StatusServiceUnavailable, "Task service not available")
		return
	}

	fixedCount := a.Tasks.ReclassifyUserTasks(email, user, aliases, msgs)
	respondJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}

func (a *API) HandleRestoreGmailCC(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)

	svc, err := channels.GetGmailService(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Gmail service error: "+err.Error())
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

	if a.Tasks == nil {
		respondError(w, http.StatusServiceUnavailable, "Task service not available")
		return
	}

	fixedCount := a.Tasks.RestoreGmailCCAssignment(r.Context(), email, user, aliases, allMsgs, svc)
	respondJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}
