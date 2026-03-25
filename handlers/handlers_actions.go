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

func HandleManualScan(w http.ResponseWriter, r *http.Request) {
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
			_ = services.FlushGamificationData() // Piggyback: 수동 스캔 후 메모리 큐 비우기
		}()
	}

	w.WriteHeader(http.StatusOK)
}
func HandleInternalScan(w http.ResponseWriter, r *http.Request) {
	if cfg.InternalScanSecret == "" {
		logger.Warnf("[INTERNAL-SCAN] Internal scan attempt for unconfigured secret from %s", r.RemoteAddr)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	secret := r.Header.Get("X-Internal-Secret")
	if secret != cfg.InternalScanSecret {
		logger.Warnf("[INTERNAL-SCAN] Unauthorized access attempt from %s", r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scanMutex.Lock()
	if isScanning {
		scanMutex.Unlock()
		logger.Warnf("[INTERNAL-SCAN] Full scan skipped: already in progress")
		respondJSON(w, map[string]string{"status": "skipped", "reason": "scan already in progress"})
		return
	}
	isScanning = true
	scanMutex.Unlock()

	defer func() {
		scanMutex.Lock()
		isScanning = false
		scanMutex.Unlock()
	}()

	// Cloud Run 모드일 때만 0~5초 사이의 랜덤한 jitter 추가 (공진 방지)
	if cfg.CloudRunMode {
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

	// Piggyback: 정기 스캔으로 DB가 활성화된 김에 메모리에 쌓인 게이미피케이션 데이터를 플러시
	if err := services.FlushGamificationData(); err != nil {
		logger.Errorf("[INTERNAL-SCAN] Gamification flush error: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Scan completed successfully")
}

func HandleTranslate(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		respondJSON(w, map[string]string{"status": "skipped", "reason": "empty language"})
		return
	}

	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 캐시 배열 오염 방지를 위해 깊은 복사 (Deep Copy)
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
		// DB 조회 실패 시, nil 맵으로 인해 모든 항목을 다시 번역하여 토큰이 폭발하는 현상 원천 차단
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
		const batchSize = 30 // 한 번에 AI로 보낼 최대 개수 (토큰 폭발 및 응답 잘림 방지)
		var totalTranslated int

		for i := 0; i < len(toTranslateIDs); i += batchSize {
			end := i + batchSize
			if end > len(toTranslateIDs) {
				end = len(toTranslateIDs)
			}
			chunk := toTranslateIDs[i:end]

			count, err := TranslateMessagesByID(r.Context(), email, chunk, lang)
			if err != nil {
				logger.Errorf("[TRANSLATE] Partial translation failed at chunk %d: %v", i, err)
				break // 에러 발생 시 중단하되, 직전까지 성공한 번역본은 DB/캐시에 유지됨
			}
			totalTranslated += count
		}
		logger.Infof("[TRANSLATE] Successfully translated %d/%d messages to %s", totalTranslated, len(toTranslateIDs), lang)
	}

	respondJSON(w, map[string]string{
		"status":           "success",
		"translated_count": fmt.Sprintf("%d", len(toTranslateIDs)),
	})
}

func HandleReclassifyOldData(w http.ResponseWriter, r *http.Request) {
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
	respondJSON(w, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}

func HandleRestoreGmailCC(w http.ResponseWriter, r *http.Request) {
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
	respondJSON(w, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}
