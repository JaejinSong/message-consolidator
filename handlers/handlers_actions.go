package handlers

import (
	"fmt"
	"math/rand"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"strings"
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

	msgs, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	archived, _, err := store.GetArchivedMessagesFiltered(email, 200, 0, "", "", "")
	if err == nil {
		msgs = append(msgs, archived...)
	}

	var idList []int
	for _, m := range msgs {
		idList = append(idList, m.ID)
	}
	existingTranslations, _ := store.GetTaskTranslationsBatch(idList, lang)

	var toTranslateIDs []int
	for _, m := range msgs {
		if _, ok := existingTranslations[m.ID]; !ok {
			toTranslateIDs = append(toTranslateIDs, m.ID)
		}
	}

	logger.Infof("[TRANSLATE] Found %d messages needing translation to %s for %s", len(toTranslateIDs), lang, email)

	if len(toTranslateIDs) > 0 {
		count, err := TranslateMessagesByID(r.Context(), email, toTranslateIDs, lang)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Infof("[TRANSLATE] Successfully translated %d/%d messages to %s", count, len(toTranslateIDs), lang)
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

	allMyIdentities := GetEffectiveAliases(*user, aliases)

	msgs, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fixedCount := 0
	for _, m := range msgs {
		rawAssignee := strings.TrimSpace(m.Assignee)
		normalizedAssignee := strings.ToLower(rawAssignee)

		if normalizedAssignee == "기타 업무" || normalizedAssignee == "기타업무" || normalizedAssignee == "other tasks" || normalizedAssignee == "미지정" {
			_ = store.UpdateTaskAssignee(email, m.ID, "")
			fixedCount++
			continue
		}

		isDirectGmail := true
		if m.Source == "gmail" {
			lowOrig := strings.ToLower(m.OriginalText)
			lowEmail := strings.ToLower(user.Email)

			toIdx := strings.Index(lowOrig, "to: ")
			ccIdx := strings.Index(lowOrig, "cc: ")
			bccIdx := strings.Index(lowOrig, "bcc: ")
			subjIdx := strings.Index(lowOrig, "subject: ")

			limitIdx := -1
			if ccIdx != -1 {
				limitIdx = ccIdx
			}
			if bccIdx != -1 && (limitIdx == -1 || bccIdx < limitIdx) {
				limitIdx = bccIdx
			}
			if subjIdx != -1 && (limitIdx == -1 || subjIdx < limitIdx) {
				limitIdx = subjIdx
			}

			isDirect := false
			if toIdx != -1 {
				toBlock := ""
				if limitIdx != -1 && limitIdx > toIdx {
					toBlock = lowOrig[toIdx:limitIdx]
				} else {
					toBlock = lowOrig[toIdx:]
				}
				if strings.Contains(toBlock, lowEmail) {
					isDirect = true
				}
			}
			isDirectGmail = isDirect
		}

		isMarkedAsMine := false
		if normalizedAssignee == "내 업무" || normalizedAssignee == "내업무" || normalizedAssignee == "my tasks" || normalizedAssignee == "mytasks" {
			isMarkedAsMine = true
		} else {
			for _, a := range allMyIdentities {
				if a != "" && strings.EqualFold(rawAssignee, a) {
					isMarkedAsMine = true
					break
				}
			}
		}

		matchedByAlias := false
		allCheckAliases := append([]string{"나", "me"}, allMyIdentities...)
		for _, a := range allCheckAliases {
			if a != "" {
				if m.Source == "gmail" {
					if isDirectGmail && IsAliasMatched(m.Task, m.Requester, a) {
						matchedByAlias = true
						break
					}
				} else {
					if IsAliasMatched(m.OriginalText, m.Requester, a) {
						matchedByAlias = true
						break
					}
				}
			}
		}

		if isMarkedAsMine {
			if m.Source == "gmail" && !isDirectGmail {
				currentAssignee := strings.ToLower(m.Assignee)
				isGeneric := currentAssignee == "내 업무" || currentAssignee == "나" || currentAssignee == "me" || currentAssignee == ""
				if isGeneric {
					_ = store.UpdateTaskAssignee(email, m.ID, "")
					fixedCount++
					continue
				}
			}

			newAssignee := m.Assignee
			changed := false

			if matchedByAlias {
				newAssignee = user.Name
				if newAssignee == "" {
					newAssignee = email
				}
				if m.Assignee != newAssignee {
					changed = true
				}
			} else {
				lowCurr := strings.ToLower(m.Assignee)
				if lowCurr == "내 업무" || lowCurr == "나" || lowCurr == "me" {
					newAssignee = ""
					changed = true
				}
			}

			if changed {
				_ = store.UpdateTaskAssignee(email, m.ID, newAssignee)
				fixedCount++
			}
			continue
		}

		if matchedByAlias && strings.TrimSpace(m.Assignee) == "" {
			assigneeName := user.Name
			if assigneeName == "" {
				assigneeName = user.Email
			}
			_ = store.UpdateTaskAssignee(email, m.ID, assigneeName)
			fixedCount++
		}
	}

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
	archivedMsgs, _, _ := store.GetArchivedMessagesFiltered(email, 10000, 0, "", "", "")

	var allMsgs []store.ConsolidatedMessage
	allMsgs = append(allMsgs, activeMsgs...)
	allMsgs = append(allMsgs, archivedMsgs...)

	fixedCount := 0
	for _, m := range allMsgs {
		if m.Source != "gmail" {
			continue
		}

		toIdx := strings.Index(m.OriginalText, "To: ")
		subjIdx := strings.Index(m.OriginalText, ", Subject: ")

		var toHeader string
		if toIdx != -1 && subjIdx != -1 && subjIdx > toIdx {
			toHeader = m.OriginalText[toIdx+4 : subjIdx]
		}

		if toHeader != "" && strings.Contains(strings.ToLower(toHeader), strings.ToLower(user.Email)) {
			if strings.TrimSpace(m.Assignee) == "" {
				assigneeName := user.Name
				if assigneeName == "" {
					assigneeName = user.Email
				}
				_ = store.UpdateTaskAssignee(email, m.ID, assigneeName)
				fixedCount++
			}
			continue
		}

		currentAssignee := strings.TrimSpace(m.Assignee)
		lowerAssignee := strings.ToLower(currentAssignee)

		isWronglyAssignedToMe := lowerAssignee == "" || lowerAssignee == "내 업무" || lowerAssignee == "내업무" || lowerAssignee == "나" || lowerAssignee == "me" || strings.EqualFold(currentAssignee, user.Name) || strings.EqualFold(currentAssignee, user.Email)

		if !isWronglyAssignedToMe {
			for _, alias := range aliases {
				if alias != "" && strings.EqualFold(currentAssignee, alias) {
					isWronglyAssignedToMe = true
					break
				}
			}
		}

		if isWronglyAssignedToMe {
			actualAssignee := ""
			if toHeader != "" {
				actualAssignee = channels.ExtractNameFromEmail(toHeader)
			}

			if actualAssignee == "" {
				msgID := m.SourceTS
				if strings.HasPrefix(msgID, "gmail-") {
					parts := strings.Split(msgID, "-")
					if len(parts) >= 2 {
						msgID = parts[1]
					}
				}

				msg, err := svc.Users.Messages.Get("me", msgID).Format("metadata").MetadataHeaders("To").Do()
				if err == nil && msg.Payload != nil {
					for _, h := range msg.Payload.Headers {
						if h.Name == "To" {
							actualAssignee = channels.ExtractNameFromEmail(h.Value)
							break
						}
					}
				}
			}

			if actualAssignee != "" && currentAssignee != actualAssignee {
				_ = store.UpdateTaskAssignee(email, m.ID, actualAssignee)
				fixedCount++
			}
		}
	}

	respondJSON(w, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}

func GetEffectiveAliases(user store.User, aliases []string) []string {
	var all []string
	if user.Name != "" {
		all = append(all, user.Name)
	}
	all = append(all, aliases...)
	return all
}

func IsAliasMatched(text, requester, alias string) bool {
	if alias == "" || text == "" {
		return false
	}
	// Case-insensitive match for name or mentions in text
	textLower := strings.ToLower(text)
	aliasLower := strings.ToLower(alias)

	if strings.Contains(textLower, "@"+aliasLower) {
		return true
	}
	// Also check if requester is NOT the alias themselves (to avoid self-assignment)
	if !strings.EqualFold(requester, alias) {
		if strings.Contains(textLower, aliasLower) {
			return true
		}
	}
	return false
}
