package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/ai/core"
	"message-consolidator/store"
	"strings"

	"golang.org/x/sync/singleflight"
)

// TranslationService centralizes AI-powered translation logic and ensures deduplication via singleflight.
type TranslationService struct {
	llm          *ai.AIClient
	requestGroup singleflight.Group
	semaphore    chan struct{}
}

func NewTranslationService(llm *ai.AIClient) *TranslationService {
	return &TranslationService{
		llm:       llm,
		semaphore: make(chan struct{}, 5),
	}
}

// Translate handles a single translation request using the AI client.
// reportID attributes report-translation cost when isReport=true; pass 0 for task-message translations.
func (s *TranslationService) Translate(ctx context.Context, email string, deduplicationKey string, text string, targetLangCode string, isReport bool, reportID store.ReportID) (string, error) {
	if s.llm == nil {
		return "", fmt.Errorf("AI service not initialized")
	}

	// any 사유: singleflight.Group.Do callback 시그니처(any 반환) — string으로 단일 타입 단정.
	val, err, _ := s.requestGroup.Do(deduplicationKey, func() (any, error) {
		// Why: Semaphore limits concurrent AI API calls to 5 to prevent rate limiting.
		s.semaphore <- struct{}{}
		defer func() { <-s.semaphore }()

		targetLangName := GetLanguageName(targetLangCode)
		if isReport {
			return s.llm.TranslateReport(ctx, email, text, targetLangName, reportID)
		}
		return s.llm.TranslateTaskMessage(ctx, email, text, targetLangName)
	})

	if err != nil {
		return "", err
	}
	result := val.(string)
	// Report translations must preserve ```json fences so the renderer can replace them with table components.
	if isReport {
		return result, nil
	}
	return core.CleanMarkdownText(result), nil
}

// TranslateBatch handles multiple tasks in a single AI call with semaphore protection.
// Why: Minimizes AI calls and costs by batching N tasks into a single structured prompt.
func (s *TranslationService) TranslateBatch(ctx context.Context, email string, tasks []store.TranslateRequest, lang string) ([]ai.TranslationResult, error) {
	if s.llm == nil || len(tasks) == 0 {
		return nil, nil
	}

	key := batchTranslationKey(email, tasks, lang)

	// any 사유: singleflight.Group.Do callback 시그니처 — []ai.TranslationResult로 단일 타입 단정.
	val, err, _ := s.requestGroup.Do(key, func() (any, error) {
		s.semaphore <- struct{}{}
		defer func() { <-s.semaphore }()
		return s.llm.TranslateTasksBatch(ctx, email, tasks, GetLanguageName(lang))
	})

	// Why: translateInChunks returns (partial, err) when a later chunk fails. Surface partial
	// successes (already token-attributed) so callers can cache them instead of double-spending.
	partial, _ := val.([]ai.TranslationResult)
	return partial, err
}

func batchTranslationKey(email string, tasks []store.TranslateRequest, lang string) string {
	payload, _ := json.Marshal(struct {
		Email string
		Lang  string
		Tasks []store.TranslateRequest
	}{email, lang, tasks})
	return fmt.Sprintf("batch-%x", sha256.Sum256(payload))
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
