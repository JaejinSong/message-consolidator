package services

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"strings"

	"golang.org/x/sync/singleflight"
)

// TranslationService centralizes AI-powered translation logic and ensures deduplication via singleflight.
type TranslationService struct {
	gemini       *ai.GeminiClient
	requestGroup singleflight.Group
}

func NewTranslationService(gemini *ai.GeminiClient) *TranslationService {
	return &TranslationService{
		gemini: gemini,
	}
}

// Translate handles a single translation request using the Gemini AI client.
// The deduplicationKey (e.g., "report_123_ko" or "task_456_th") ensures concurrent requests 
// for the same content and language only trigger one AI call.
func (s *TranslationService) Translate(ctx context.Context, email string, deduplicationKey string, text string, targetLangCode string, isReport bool) (string, error) {
	if s.gemini == nil {
		return "", fmt.Errorf("AI service not initialized")
	}

	val, err, _ := s.requestGroup.Do(deduplicationKey, func() (interface{}, error) {
		targetLangName := GetLanguageName(targetLangCode)
		
		var translated string
		var err error

		if isReport {
			// Why: Use TranslateReport for Markdown-heavy content like Weekly Reports.
			translated, err = s.gemini.TranslateReport(ctx, email, text, targetLangName)
		} else {
			// Why: Use TranslateTaskMessage for short, conversational content (Slack/WhatsApp/Email).
			translated, err = s.gemini.TranslateTaskMessage(ctx, email, text, targetLangName)
		}

		if err != nil {
			return nil, err
		}

		return translated, nil
	})

	if err != nil {
		return "", err
	}

	return val.(string), nil
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
