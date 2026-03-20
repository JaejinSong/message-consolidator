package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// applyTranslations는 추출된 메시지 배열에 번역본을 매핑하는 공통 헬퍼 함수입니다.
func applyTranslations(msgs []store.ConsolidatedMessage, lang string) {
	if lang == "" || len(msgs) == 0 {
		return
	}
	ids := make([]int, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	translations, err := store.GetTaskTranslationsBatch(ids, lang)
	if err == nil {
		for i := range msgs {
			if t, ok := translations[msgs[i].ID]; ok {
				msgs[i].Task = t
			}
		}
	}
}

// 공통 헬퍼: HTTP 요청에서 JSON 파싱 및 Body 안전하게 닫기 (메모리 누수 방지)
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// 공통 헬퍼: HTTP 응답에 JSON 포맷으로 쓰기
func respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func handleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	lang := r.URL.Query().Get("lang")

	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create a copy of the slice to avoid polluting the global cache when we apply translations
	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	applyTranslations(msgs, lang) // 통합된 함수 사용

	respondJSON(w, msgs)
}

func handleMarkDone(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID   int  `json:"id"`
		Done bool `json:"done"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := store.MarkMessageDone(email, req.ID, req.Done); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetArchived(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	lang := r.URL.Query().Get("lang")
	sort := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	if limit <= 0 {
		limit = 50
	}

	msgsRaw, total, err := store.GetArchivedMessagesFiltered(email, limit, offset, q, sort, order)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create a copy to avoid cache pollution
	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	applyTranslations(msgs, lang) // 통합된 함수 사용

	respondJSON(w, map[string]interface{}{
		"messages": msgs,
		"total":    total,
	})
}

func handleGetArchivedCount(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")

	_, total, err := store.GetArchivedMessagesFiltered(email, 1, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]int{"total": total})
}

func handleExportExcel(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")

	msgs, _, err := store.GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	f := excelize.NewFile()
	defer f.Close()

	sheet := "Tasks"
	index, _ := f.NewSheet(sheet)
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1")

	headers := []string{"ID", "Source", "Room", "Task", "Requester", "Assignee", "Assigned At", "Created At", "Completed At"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E0E0E0"}, Pattern: 1},
	})
	f.SetRowStyle(sheet, 1, 1, style)

	for i, m := range msgs {
		row := i + 2
		compAt := ""
		if m.CompletedAt != nil {
			compAt = m.CompletedAt.Format("2006-01-02 15:04:05")
		}

		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), m.ID)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), m.Source)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), m.Room)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), m.Task)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), m.Requester)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), m.Assignee)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), m.AssignedAt)
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), m.CreatedAt.Format("2006-01-02 15:04:05"))
		f.SetCellValue(sheet, fmt.Sprintf("I%d", row), compAt)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("Message_Archive_%s.xlsx", timestamp)

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filename))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
	if err := f.Write(w); err != nil {
		logger.Errorf("Failed to write excel: %v", err)
	}
}

func handleExportArchive(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")

	msgs, _, err := store.GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("Message_Archive_%s.csv", timestamp)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filename))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

	w.Write([]byte("\xEF\xBB\xBF"))
	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"ID", "Source", "Room", "Task", "Requester", "Assignee", "Assigned At", "Created At", "Completed At"})

	for _, m := range msgs {
		compAt := ""
		if m.CompletedAt != nil {
			compAt = m.CompletedAt.Format("2006-01-02 15:04:05")
		}
		writer.Write([]string{
			fmt.Sprintf("%d", m.ID),
			m.Source,
			m.Room,
			m.Task,
			m.Requester,
			m.Assignee,
			m.AssignedAt,
			m.CreatedAt.Format("2006-01-02 15:04:05"),
			compAt,
		})
	}
}

func handleExportJSON(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")

	msgs, _, err := store.GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("Message_Archive_%s.json", timestamp)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filename))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ") // JSON을 읽기 쉽도록 예쁘게 포맷팅
	if err := encoder.Encode(msgs); err != nil {
		logger.Errorf("Failed to write json export: %v", err)
	}
}

func handleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	status := GetWhatsAppStatus(email)
	respondJSON(w, map[string]string{"status": status})
}

func handleManualScan(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "Korean"
	}
	logger.Debugf("Manual scan triggered via API for %s (lang: %s)", email, lang)

	go func() {
		scan(email, lang)
		store.PersistAllScanMetadata(email)
	}()

	w.WriteHeader(http.StatusOK)
}

func handleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	qr, err := GetWhatsAppQR(r.Context(), email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]string{"qr": qr})
}

func handleTranslate(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
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

	// Also fetch recent archived messages for translation
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

// TranslateMessagesByID is a helper to translate specific messages for a user
func TranslateMessagesByID(ctx context.Context, email string, ids []int, lang string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// 1. Get detailed message data for these IDs
	var toTranslate []store.TranslateRequest
	for _, id := range ids {
		// We can get from DB directly to ensure we have the latest
		m, err := store.GetMessageByID(id)
		if err != nil {
			logger.Warnf("[TRANSLATE] Failed to get message ID %d for %s: %v", id, email, err)
			continue
		}
		toTranslate = append(toTranslate, store.TranslateRequest{
			ID:           m.ID,
			Text:         m.Task,
			OriginalText: m.OriginalText,
		})
	}

	if len(toTranslate) == 0 {
		return 0, nil
	}

	// 2. Call Gemini
	gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[TRANSLATE] Failed to init Gemini client: %v", err)
		return 0, err
	}

	translations, err := gc.Translate(ctx, email, toTranslate, lang)
	if err != nil {
		logger.Errorf("[TRANSLATE] Gemini Translation Error for %s: %v", email, err)
		return 0, err
	}

	// 3. Save
	count := 0
	for _, t := range translations {
		if err := store.SaveTaskTranslation(t.ID, lang, t.Text); err == nil {
			count++
		} else {
			logger.Errorf("[TRANSLATE] Failed to save translation for ID %d (%s): %v", t.ID, lang, err)
		}
	}

	return count, nil
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID  int   `json:"id"`
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ids := req.IDs
	if len(ids) == 0 && req.ID != 0 {
		ids = []int{req.ID}
	}

	for _, id := range ids {
		store.DeleteMessage(email, id)
	}
	w.WriteHeader(http.StatusOK)
}

func handleHardDelete(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, id := range req.IDs {
		store.HardDeleteMessage(email, id)
	}
	w.WriteHeader(http.StatusOK)
}

func handleRestore(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, id := range req.IDs {
		store.RestoreMessage(email, id)
	}
	w.WriteHeader(http.StatusOK)
}

func handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID   int    `json:"id"`
		Task string `json:"task"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.UpdateTaskText(email, req.ID, req.Task); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	aliases, err := store.GetUserAliases(user.ID)
	if err == nil {
		user.Aliases = aliases
	}

	if len(user.Aliases) == 0 {
		sc := NewSlackClient(cfg.SlackToken)
		slackUser, err := sc.LookupUserByEmail(user.Email)
		if err == nil && slackUser != nil {
			store.UpdateUserSlackID(user.Email, slackUser.ID)
			store.AddUserAlias(user.ID, slackUser.RealName)
			if slackUser.Profile.DisplayName != "" {
				store.AddUserAlias(user.ID, slackUser.Profile.DisplayName)
			}
			user.Aliases, _ = store.GetUserAliases(user.ID)
		}
	}

	respondJSON(w, user)
}

func handleGetUserAliases(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliases, err := store.GetUserAliases(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, aliases)
}

func handleAddAlias(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := store.AddUserAlias(user.ID, req.Alias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := store.DeleteUserAlias(user.ID, req.Alias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetTenantAliases(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	aliases, err := store.GetTenantAliases(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, aliases)
}

func handleAddTenantAlias(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		Original string `json:"original"`
		Primary  string `json:"primary"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.AddTenantAlias(email, req.Original, req.Primary); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleDeleteTenantAlias(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		Original string `json:"original"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.DeleteTenantAlias(email, req.Original); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetTokenUsage(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	todayTotal, err := store.GetDailyTokenUsage(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]int{"todayTotal": todayTotal})
}

func handleGetMappings(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	mappings, err := store.GetContactsMappings(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, mappings)
}

func handleAddMapping(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		RepName string `json:"rep_name"`
		Aliases string `json:"aliases"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.AddContactMapping(email, req.RepName, req.Aliases); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleReclassifyOldData(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliases, _ := store.GetUserAliases(user.ID)

	allMyIdentities := getEffectiveAliases(*user, aliases)

	msgs, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fixedCount := 0
	for _, m := range msgs {
		rawAssignee := strings.TrimSpace(m.Assignee)
		normalizedAssignee := strings.ToLower(rawAssignee)

		// 1. "기타 업무", "미지정" 등 불필요한 더미 데이터는 아예 빈칸으로 초기화
		if normalizedAssignee == "기타 업무" || normalizedAssignee == "기타업무" || normalizedAssignee == "other tasks" || normalizedAssignee == "미지정" {
			_ = store.UpdateTaskAssignee(email, m.ID, "")
			fixedCount++
			continue
		}

		// Gmail인 경우 직접 수신인(To:) 여부 미리 확인
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
			// 이미 내 별칭 중 하나로 할당되어 있다면 '내 업무'로 간주 (보존 대상)
			for _, a := range allMyIdentities {
				if a != "" && strings.EqualFold(rawAssignee, a) {
					isMarkedAsMine = true
					break
				}
			}
		}

		// 현재 별칭들 + 기본 별칭(나, me)으로 다시 매칭 시도
		matchedByAlias := false
		allCheckAliases := append([]string{"나", "me"}, allMyIdentities...)
		for _, a := range allCheckAliases {
			if a != "" {
				// Gmail인 경우 OriginalText에 헤더가 포함되므로 Task(제목) 위주로 매칭하되,
				// 직접 수신인인 경우에만 alias 매칭을 인정함
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

		// 1. "내 업무" 레이블 처리
		if isMarkedAsMine {
			// Gmail인 경우 CC/BCC 체크로 걸러내기
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

			// matchedByAlias를 그대로 사용하여 "나", "me" 등 정상적인 키워드 매칭도 보호
			if matchedByAlias {
				newAssignee = user.Name
				if newAssignee == "" {
					newAssignee = email
				}
				if m.Assignee != newAssignee {
					changed = true
				}
			} else {
				// If not matched, and it was a generic label, clear it
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

		// 2. 담당자가 지정되지 않은 업무 중, 내 별칭과 매칭되는 경우에만 내 업무로 복구 (타인 업무 덮어쓰기 방지)
		if matchedByAlias && strings.TrimSpace(m.Assignee) == "" {
			// Gmail인 경우 이미 isDirectGmail 위에서 체크됨
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

func handleRestoreGmailCC(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)

	// Gmail API 서비스 가져오기 (Fallback 용)
	svc, err := GetGmailService(r.Context(), email)
	if err != nil {
		http.Error(w, "Gmail service error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	user, _ := store.GetOrCreateUser(email, "", "")
	aliases, _ := store.GetUserAliases(user.ID)

	// 현재 활성화된 업무 + 보관함(Archive) 업무 모두 가져오기
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

		// 1. OriginalText에서 To: 헤더 고속 추출 시도
		toIdx := strings.Index(m.OriginalText, "To: ")
		subjIdx := strings.Index(m.OriginalText, ", Subject: ")

		var toHeader string
		if toIdx != -1 && subjIdx != -1 && subjIdx > toIdx {
			toHeader = m.OriginalText[toIdx+4 : subjIdx]
		}

		// 만약 To 헤더에 내 이메일이 포함되어 있다면 직접 받은 메일(CC 아님)이므로 건너뜀
		if toHeader != "" && strings.Contains(strings.ToLower(toHeader), strings.ToLower(user.Email)) {
			// 단, 담당자가 비어있다면 내 이름으로 보완해 줌 (보너스 복구 기능)
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

		// CC로 수신된 메일(To에 내가 없음) 판별
		// 현재 담당자가 비어있거나 '나(사용자)'로 잘못 지정된 경우
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

		// 내가 CC로 받았는데 내게 할당된 상태라면 정제 타겟!
		if isWronglyAssignedToMe {
			actualAssignee := ""
			if toHeader != "" {
				actualAssignee = extractNameFromEmail(toHeader)
			}

			// OriginalText 파싱 실패 시에만 최후의 수단으로 Gmail API 직접 호출 (Fallback)
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
							actualAssignee = extractNameFromEmail(h.Value)
							break
						}
					}
				}
			}

			// 실제 To 수신자로 담당자 강제 교체
			if actualAssignee != "" && currentAssignee != actualAssignee {
				_ = store.UpdateTaskAssignee(email, m.ID, actualAssignee)
				fixedCount++
			}
		}
	}

	respondJSON(w, map[string]interface{}{"status": "success", "fixed_count": fixedCount})
}
