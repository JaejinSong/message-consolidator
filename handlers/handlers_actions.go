package handlers

import (
	"context"
	"fmt"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"sync"

	"google.golang.org/api/gmail/v1"
)

type fixedCountResponse struct {
	Status     string `json:"status"`
	FixedCount int    `json:"fixed_count"`
}

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
		go func() { //nolint:contextcheck // Async scan outlives the request; uses Background ctx by design.
			defer safego.Recover("manual-scan")
			ScanFunc(email, lang)
			store.PersistAllScanMetadata(context.Background(), email)
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

	logger.Infof("[INTERNAL-SCAN] Starting full scan triggered via API")
	if FullScanFunc != nil {
		FullScanFunc()
	} else {
		logger.Errorf("[INTERNAL-SCAN] FullScanFunc not initialized")
		respondError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Scan completed successfully")

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

	toTranslateIDs, err := a.filterUntranslatedIDs(r.Context(), msgs, lang)
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
	msgsRaw, err := store.GetMessages(ctx, email)
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
func (a *API) filterUntranslatedIDs(ctx context.Context, msgs []store.ConsolidatedMessage, lang string) ([]store.MessageID, error) {
	//Why: Deduplicates message IDs before processing to avoid redundant translation requests and minimize AI API expenses.
	uniqueIDs := make(map[store.MessageID]bool)
	var idList []store.MessageID
	for _, m := range msgs {
		if !uniqueIDs[m.ID] {
			uniqueIDs[m.ID] = true
			idList = append(idList, m.ID)
		}
	}

	existingTranslations, err := store.GetTaskTranslationsBatch(ctx, idList, lang)
	if err != nil {
		return nil, err
	}

	var toTranslateIDs []store.MessageID
	for _, id := range idList {
		if _, ok := existingTranslations[id]; !ok {
			toTranslateIDs = append(toTranslateIDs, id)
		}
	}
	return toTranslateIDs, nil
}

// Why: Chunks large translation requests into manageable batches to stay within token limits and handle partial failures gracefully.
func (a *API) processTranslationBatches(ctx context.Context, email string, toTranslateIDs []store.MessageID, lang string) {
	const batchSize = 30
	var totalTranslated int

	for i := 0; i < len(toTranslateIDs); i += batchSize {
		end := i + batchSize
		if end > len(toTranslateIDs) {
			end = len(toTranslateIDs)
		}
		chunk := toTranslateIDs[i:end]

		results, err := a.Tasks.ProcessBatchTranslation(ctx, email, chunk, lang)
		if err != nil {
			logger.Errorf("[TRANSLATE] Batch translation failed at chunk %d: %v", i, err)
			break
		}
		for _, r := range results {
			if r.Success {
				totalTranslated++
			}
		}
	}
	logger.Infof("[TRANSLATE] Successfully translated %d/%d messages to %s", totalTranslated, len(toTranslateIDs), lang)
}

func (a *API) HandleReclassifyOldData(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(r.Context(), email, "", "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	aliases, _ := store.GetUserAliases(r.Context(), user.ID)

	msgs, err := store.GetMessages(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if a.Tasks == nil {
		respondError(w, http.StatusServiceUnavailable, "Task service not available")
		return
	}

	fixedCount := a.Tasks.ReclassifyUserTasks(r.Context(), email, user, aliases, msgs)
	respondJSON(w, http.StatusOK, fixedCountResponse{Status: "success", FixedCount: fixedCount})
}

func (a *API) HandleRestoreGmailCC(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	svc, err := channels.GetGmailService(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Gmail service error: "+err.Error())
		return
	}

	user, _ := store.GetOrCreateUser(r.Context(), email, "", "")
	aliases, _ := store.GetUserAliases(r.Context(), user.ID)
	if a.Tasks == nil {
		respondError(w, http.StatusServiceUnavailable, "Task service not available")
		return
	}

	totalFixed := a.processActiveRestore(r.Context(), email, user, aliases, svc)
	archiveFixed := a.processArchiveRestore(r.Context(), email, user, aliases, svc)
	
	respondJSON(w, http.StatusOK, fixedCountResponse{Status: "success", FixedCount: totalFixed + archiveFixed})
}

func (a *API) processActiveRestore(ctx context.Context, email string, user *store.User, aliases []string, svc *gmail.Service) int {
	activeMsgs, _ := store.GetMessages(ctx, email)
	if len(activeMsgs) == 0 {
		return 0
	}

	updates, count := a.Tasks.RestoreGmailCCAssignment(ctx, email, user, aliases, activeMsgs, svc)
	if count > 0 {
		_ = store.UpdateTaskAssigneesBatch(ctx, email, updates)
	}
	return count
}

func (a *API) processArchiveRestore(ctx context.Context, email string, user *store.User, aliases []string, svc *gmail.Service) int {
	filter := store.ArchiveFilter{Email: email}
	total, _ := store.GetArchivedMessagesCount(ctx, filter)
	if total == 0 {
		return 0
	}

	totalFixed := 0
	const chunkSize = 50
	for offset := 0; offset < total; offset += chunkSize {
		filter.Limit = chunkSize
		filter.Offset = offset
		msgs, _, _ := store.GetArchivedMessagesFiltered(ctx, filter)
		if len(msgs) == 0 {
			break
		}

		updates, count := a.Tasks.RestoreGmailCCAssignment(ctx, email, user, aliases, msgs, svc)
		if count > 0 {
			_ = store.UpdateTaskAssigneesBatch(ctx, email, updates)
			totalFixed += count
		}
	}
	return totalFixed
}

func (a *API) HandleInvalidateCache(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	store.InvalidateCache(email)
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "email": email})
}
