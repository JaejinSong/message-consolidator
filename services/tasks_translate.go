package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

// BatchTranslateResult represents the status of a single task translation within a batch request.
type BatchTranslateResult struct {
	ID             store.MessageID `json:"id"`
	Success        bool            `json:"success"`
	TranslatedText string          `json:"translated_text,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// translationPayload is the JSON structure stored in task_translations.translated_text
// when subtasks are present. Plain strings (legacy) are still supported.
type translationPayload struct {
	T string   `json:"t"`
	S []string `json:"s,omitempty"`
}

func (s *TasksService) GetTranslationService() *TranslationService {
	return s.translationSvc
}

func (s *TasksService) ApplyTranslations(ctx context.Context, email, lang string, msgs []store.ConsolidatedMessage) {
	if lang == "" || strings.EqualFold(lang, "en") || len(msgs) == 0 {
		return
	}
	ids := make([]store.MessageID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	translations, _ := store.GetTaskTranslationsBatch(ctx, ids, lang)
	var missingIDs []store.MessageID
	for i := range msgs {
		if raw, ok := translations[msgs[i].ID]; ok {
			mainTask, subTexts := parseTranslatedText(raw)
			msgs[i].Task = mainTask
			if len(subTexts) > 0 && len(msgs[i].Subtasks) == len(subTexts) {
				for j := range msgs[i].Subtasks {
					msgs[i].Subtasks[j].Task = subTexts[j]
				}
			}
		} else {
			missingIDs = append(missingIDs, msgs[i].ID)
		}
	}
	s.triggerJITTranslation(email, lang, missingIDs) //nolint:contextcheck // Async fan-out uses Background ctx with its own timeout.
}

func (s *TasksService) triggerJITTranslation(email, lang string, ids []store.MessageID) {
	if len(ids) == 0 {
		return
	}
	// Why: Asynchronously triggers JIT translation to avoid blocking the main data request.
	go func() {
		defer safego.Recover("jit-translation")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _ = s.ProcessBatchTranslation(ctx, email, ids, lang)
	}()
}

func (s *TasksService) ProcessBatchTranslation(ctx context.Context, email string, taskIDs []store.MessageID, lang string) ([]BatchTranslateResult, error) {
	if s.translationSvc == nil {
		return nil, fmt.Errorf("service not ready")
	}

	cached, _ := store.GetTaskTranslationsBatch(ctx, taskIDs, lang)
	missingIDs := FilterMissingIDs(taskIDs, cached)

	newTrans := make(map[store.MessageID]string)
	if len(missingIDs) > 0 {
		var err error
		newTrans, err = s.executeBatchTranslation(ctx, email, missingIDs, lang)
		if err != nil {
			logger.Warnf("[TASKS] batch translation failed: %v", err)
		}
	}

	return s.mergeBatchResults(taskIDs, cached, newTrans), nil
}

// FilterMissingIDs returns IDs not present in cached.
func FilterMissingIDs(all []store.MessageID, cached map[store.MessageID]string) []store.MessageID {
	var missing []store.MessageID
	for _, id := range all {
		if _, ok := cached[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// PrepareTranslateRequests loads message text for each id and builds TranslateRequests.
func (s *TasksService) PrepareTranslateRequests(ctx context.Context, email string, ids []store.MessageID) []store.TranslateRequest {
	return s.prepareTranslateRequests(ctx, email, ids)
}

func (s *TasksService) executeBatchTranslation(ctx context.Context, email string, ids []store.MessageID, lang string) (map[store.MessageID]string, error) {
	reqs := s.prepareTranslateRequests(ctx, email, ids)
	if len(reqs) == 0 {
		return nil, nil
	}

	// Why: TranslateBatch may return (partial, err) when a later chunk fails — keep the
	// successful prefix so prior chunks' token spend isn't wasted on next JIT retry.
	results, err := s.translationSvc.TranslateBatch(ctx, email, reqs, lang)

	batchMap := make(map[store.MessageID]string)
	for _, r := range results {
		if r.Error == "" {
			batchMap[r.MessageID] = r.Text
		}
	}

	_ = store.SaveTaskTranslationsBulk(ctx, lang, batchMap)
	return batchMap, err
}

func (s *TasksService) prepareTranslateRequests(ctx context.Context, email string, ids []store.MessageID) []store.TranslateRequest {
	var reqs []store.TranslateRequest
	for _, id := range ids {
		msg, err := store.GetMessageByID(ctx, store.GetDB(), email, id)
		if err != nil {
			continue
		}
		reqs = append(reqs, BuildTranslateRequest(id, msg.Task, msg.Subtasks))
	}
	return reqs
}

func (s *TasksService) mergeBatchResults(ids []store.MessageID, cached, newTrans map[store.MessageID]string) []BatchTranslateResult {
	final := make([]BatchTranslateResult, len(ids))
	for i, id := range ids {
		text, ok := cached[id]
		if !ok {
			text = newTrans[id]
		}

		success := text != ""
		final[i] = BatchTranslateResult{ID: id, Success: success, TranslatedText: text}
		if !success {
			final[i].Error = "translation missing"
		}
	}
	return final
}

// BuildTranslateRequest encodes task + subtask texts into a single TranslateRequest.
// The Text field uses JSON when subtasks exist so the translator receives structured content.
func BuildTranslateRequest(id store.MessageID, task string, subtasks []store.Subtask) store.TranslateRequest {
	if len(subtasks) == 0 {
		return store.TranslateRequest{ID: id, Text: task}
	}
	subs := make([]string, len(subtasks))
	for i, s := range subtasks {
		subs[i] = s.Task
	}
	p := translationPayload{T: task, S: subs}
	b, err := json.Marshal(p)
	if err != nil {
		return store.TranslateRequest{ID: id, Text: task}
	}
	return store.TranslateRequest{ID: id, Text: string(b)}
}

// parseTranslatedText parses a stored translated_text value (plain or JSON).
// Returns (mainTask, subtaskTexts).
func parseTranslatedText(raw string) (string, []string) {
	if len(raw) == 0 || raw[0] != '{' {
		return raw, nil
	}
	var p translationPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return raw, nil
	}
	return p.T, p.S
}
