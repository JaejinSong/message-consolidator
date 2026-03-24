package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
)

// 공통 헬퍼: HTTP 요청에서 JSON 파싱 및 Body 안전하게 닫기 (메모리 누수 방지)
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// respondJSON 공통 헬퍼: HTTP 응답에 JSON 포맷으로 쓰기
func respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// respondError 공통 헬퍼: 에러 응답 처리 및 로깅 중앙화
func respondError(w http.ResponseWriter, code int, message string, err error) {
	if errors.Is(err, context.Canceled) {
		http.Error(w, "Client Closed Request", 499)
		return
	}
	if err != nil {
		logger.Errorf("[API-ERROR] %s: %v", message, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// TranslateMessagesByID is a helper to translate specific messages for a user
func TranslateMessagesByID(ctx context.Context, email string, ids []int, lang string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// 1. Get detailed message data for these IDs in batch
	messages, err := store.GetMessagesByIDs(ctx, ids)
	if err != nil {
		logger.Errorf("[TRANSLATE] Failed to get messages batch for %s: %v", email, err)
		return 0, err
	}

	var toTranslate []store.TranslateRequest
	for _, m := range messages {
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
	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
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
