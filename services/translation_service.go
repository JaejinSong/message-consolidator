package services

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/store"
	"strings"

	"golang.org/x/sync/singleflight"
)

// TranslationService centralizes AI-powered translation logic and ensures deduplication via singleflight.
type TranslationService struct {
	gemini       *ai.GeminiClient
	requestGroup singleflight.Group
	semaphore    chan struct{}
}

func NewTranslationService(gemini *ai.GeminiClient) *TranslationService {
	return &TranslationService{
		gemini:    gemini,
		semaphore: make(chan struct{}, 5),
	}
}

// Translate handles a single translation request using the Gemini AI client.
func (s *TranslationService) Translate(ctx context.Context, email string, deduplicationKey string, text string, targetLangCode string, isReport bool) (string, error) {
	if s.gemini == nil {
		return "", fmt.Errorf("AI service not initialized")
	}

	// any 사유: singleflight.Group.Do callback 시그니처(any 반환) — string으로 단일 타입 단정.
	val, err, _ := s.requestGroup.Do(deduplicationKey, func() (any, error) {
		// Why: Semaphore limits concurrent AI API calls to 5 to prevent rate limiting.
		s.semaphore <- struct{}{}
		defer func() { <-s.semaphore }()

		targetLangName := GetLanguageName(targetLangCode)
		if isReport {
			return s.gemini.TranslateReport(ctx, email, text, targetLangName)
		}
		return s.gemini.TranslateTaskMessage(ctx, email, text, targetLangName)
	})

	if err != nil {
		return "", err
	}
	result := val.(string)
	// Report translations must preserve ```json fences so the renderer can replace them with table components.
	if isReport {
		return result, nil
	}
	return ai.CleanMarkdownText(result), nil
}

// TranslateBatch handles multiple tasks in a single AI call with semaphore protection.
// Why: Minimizes AI calls and costs by batching N tasks into a single structured prompt.
func (s *TranslationService) TranslateBatch(ctx context.Context, email string, tasks []store.TranslateRequest, lang string) ([]ai.TranslationResult, error) {
	if s.gemini == nil || len(tasks) == 0 { return nil, nil }
	
	ids := make([]store.MessageID, len(tasks))
	for i, t := range tasks { ids[i] = t.ID }
	key := fmt.Sprintf("batch-%s-%v", lang, ids)

	// any 사유: singleflight.Group.Do callback 시그니처 — []ai.TranslationResult로 단일 타입 단정.
	val, err, _ := s.requestGroup.Do(key, func() (any, error) {
		s.semaphore <- struct{}{}
		defer func() { <-s.semaphore }()
		return s.gemini.TranslateTasksBatch(ctx, email, tasks, GetLanguageName(lang))
	})
	
	if err != nil { return nil, err }
	return val.([]ai.TranslationResult), nil
}

// GetLanguageName maps ISO 639-1 language codes to descriptive names for the AI prompt.
func GetLanguageName(code string) string {
	switch strings.ToLower(code) {
	case "ko":
		return "Korean"
	case "en":
		return "English"
	case "id":
		return "Indonesian"
	case "th":
		return "Thai"
	default:
		return code
	}
}
