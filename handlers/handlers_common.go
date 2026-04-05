package handlers

import (
	"context"
	"encoding/json"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
)

// decodeJSON is a common helper that parses JSON from an HTTP request and safely closes the Body to prevent memory leaks.
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// TranslateMessagesByID is a helper to translate specific messages for a user
func (a *API) TranslateMessagesByID(ctx context.Context, email string, ids []int, lang string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	//Why: Fetches full message details in a single batch to minimize database roundtrips.
	messages, err := store.GetMessagesByIDs(ctx, email, ids)
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

	//Why: Initializes the Gemini client to perform the actual AI-powered translation.
	gc, err := ai.NewGeminiClient(ctx, a.Config.GeminiAPIKey, a.Config.GeminiAnalysisModel, a.Config.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[TRANSLATE] Failed to init Gemini client: %v", err)
		return 0, err
	}

	translations, err := gc.Translate(ctx, email, toTranslate, lang)
	if err != nil {
		logger.Errorf("[TRANSLATE] Gemini Translation Error for %s: %v", email, err)
		return 0, err
	}

	//Why: Iterates through the returned translations and persists each one to the database.
	count := 0
	for _, t := range translations {
		if err := store.SaveTaskTranslation(ctx, t.ID, lang, t.Text); err == nil {
			count++
		} else {
			logger.Errorf("[TRANSLATE] Failed to save translation for ID %d (%s): %v", t.ID, lang, err)
		}
	}

	return count, nil
}
