package handlers

import (
	"context"
	"encoding/json"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
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

// TranslateMessagesByID is a helper to translate specific messages for a user
func TranslateMessagesByID(ctx context.Context, email string, ids []int, lang string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// 1. Get detailed message data for these IDs
	var toTranslate []store.TranslateRequest
	for _, id := range ids {
		m, err := store.GetMessageByID(ctx, id)
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
