package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"
)

func handleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	
	msgsRaw, err := GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create a copy of the slice to avoid polluting the global cache when we apply translations
	msgs := make([]ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	if lang != "" && len(msgs) > 0 {
		ids := make([]int, len(msgs))
		for i, m := range msgs {
			ids[i] = m.ID
		}
		translations, err := GetTaskTranslationsBatch(ids, lang)
		if err == nil {
			for i := range msgs {
				if t, ok := translations[msgs[i].ID]; ok {
					msgs[i].Task = t
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func handleMarkDone(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID   int  `json:"id"`
		Done bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := MarkMessageDone(email, req.ID, req.Done); err != nil {
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

	msgsRaw, total, err := GetArchivedMessagesFiltered(email, limit, offset, q, sort, order)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create a copy to avoid cache pollution
	msgs := make([]ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	if lang != "" && len(msgs) > 0 {
		ids := make([]int, len(msgs))
		for i, m := range msgs {
			ids[i] = m.ID
		}
		translations, err := GetTaskTranslationsBatch(ids, lang)
		if err == nil {
			for i := range msgs {
				if t, ok := translations[msgs[i].ID]; ok {
					msgs[i].Task = t
				}
			}
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": msgs,
		"total":    total,
	})
}

func handleGetArchivedCount(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")
	
	_, total, err := GetArchivedMessagesFiltered(email, 1, 0, q, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"total": total})
}

func handleExportExcel(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")

	msgs, _, err := GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
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
		errorf("Failed to write excel: %v", err)
	}
}

func handleExportArchive(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	q := r.URL.Query().Get("q")
	
	msgs, _, err := GetArchivedMessagesFiltered(email, 10000, 0, q, "", "")
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

func handleWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	status := GetWhatsAppStatus(email)
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func handleManualScan(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "Korean"
	}
	debugf("Manual scan triggered via API for %s (lang: %s)", email, lang)
	go scan(email, lang)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "scan started", "lang": lang})
}

func handleWhatsAppQR(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	qr, err := GetWhatsAppQR(r.Context(), email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"qr": qr})
}

func handleTranslate(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "skipped", "reason": "empty language"})
		return
	}

	msgs, err := GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Also fetch recent archived messages for translation
	archived, _, err := GetArchivedMessagesFiltered(email, 200, 0, "", "", "")
	if err == nil {
		msgs = append(msgs, archived...)
	}

	var idList []int
	for _, m := range msgs {
		idList = append(idList, m.ID)
	}
	existingTranslations, _ := GetTaskTranslationsBatch(idList, lang)

	var toTranslateIDs []int
	for _, m := range msgs {
		if _, ok := existingTranslations[m.ID]; !ok {
			toTranslateIDs = append(toTranslateIDs, m.ID)
		}
	}

	infof("[TRANSLATE] Found %d messages needing translation to %s for %s", len(toTranslateIDs), lang, email)

	if len(toTranslateIDs) > 0 {
		count, err := TranslateMessagesByID(r.Context(), email, toTranslateIDs, lang)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		infof("[TRANSLATE] Successfully translated %d/%d messages to %s", count, len(toTranslateIDs), lang)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
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
	var toTranslate []TranslateRequest
	for _, id := range ids {
		// We can get from DB directly to ensure we have the latest
		var m ConsolidatedMessage
		err := db.QueryRow("SELECT id, task, COALESCE(original_text, '') FROM messages WHERE id = $1", id).Scan(&m.ID, &m.Task, &m.OriginalText)
		if err != nil {
			continue
		}
		toTranslate = append(toTranslate, TranslateRequest{
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
		return 0, err
	}

	translations, err := gc.Translate(ctx, toTranslate, lang)
	if err != nil {
		return 0, err
	}

	// 3. Save
	count := 0
	for _, t := range translations {
		if err := SaveTaskTranslation(t.ID, lang, t.Text); err == nil {
			count++
		} else {
			errorf("[TRANSLATE] Failed to save translation for ID %d (%s): %v", t.ID, lang, err)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ids := req.IDs
	if len(ids) == 0 && req.ID != 0 {
		ids = []int{req.ID}
	}

	for _, id := range ids {
		DeleteMessage(email, id)
	}
	w.WriteHeader(http.StatusOK)
}

func handleHardDelete(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, id := range req.IDs {
		HardDeleteMessage(email, id)
	}
	w.WriteHeader(http.StatusOK)
}

func handleRestore(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, id := range req.IDs {
		RestoreMessage(email, id)
	}
	w.WriteHeader(http.StatusOK)
}

func handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		ID   int    `json:"id"`
		Task string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := UpdateTaskText(email, req.ID, req.Task); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	aliases, err := GetUserAliases(user.ID)
	if err == nil {
		user.Aliases = aliases
	}

	if len(user.Aliases) == 0 {
		sc := NewSlackClient(cfg.SlackToken)
		slackUser, err := sc.LookupUserByEmail(user.Email)
		if err == nil && slackUser != nil {
			UpdateUserSlackID(user.Email, slackUser.ID)
			AddUserAlias(user.ID, slackUser.RealName)
			if slackUser.Profile.DisplayName != "" {
				AddUserAlias(user.ID, slackUser.Profile.DisplayName)
			}
			user.Aliases, _ = GetUserAliases(user.ID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func handleGetUserAliases(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliases, err := GetUserAliases(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(aliases)
}

func handleAddAlias(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := AddUserAlias(user.ID, req.Alias); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := DeleteUserAlias(user.ID, req.Alias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

