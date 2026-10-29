package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranslateBatchRejectsStaleSourceWithMixedCache(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	email := testutil.RandomEmail("source-revision")
	ids := make([]store.MessageID, 2)
	for i, title := range []string{"Current B", "Stable"} {
		result, err := store.GetDB().ExecContext(ctx, "INSERT INTO messages(user_email, task, source, source_ts) VALUES(?, ?, 'slack', ?)", email, title, testutil.RandomTS(title))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		ids[i] = store.MessageID(id)
	}
	if err := store.SaveTaskTranslationsBulk(ctx, "ko", map[store.MessageID]string{ids[0]: "B translation", ids[1]: "Stable translation"}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(struct {
		IDs     []store.MessageID          `json:"task_ids"`
		Lang    string                     `json:"lang"`
		Sources map[store.MessageID]string `json:"source_titles"`
	}{ids, "ko", map[store.MessageID]string{ids[0]: "Old A", ids[1]: "Stable"}})
	req := httptest.NewRequest("POST", "/api/tasks/translate-batch", bytes.NewReader(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	client, err := ai.NewAIClient(ai.ProviderConfig{DeepSeekAPIKey: "test-key", DeepSeekTranslationModel: "test-model", DeepSeekBaseURL: testutil.NewChatCompletionServer(t, func(r *http.Request) (string, error) {
		t.Error("stale source must not reach AI")
		return "[]", nil
	}),
	})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Tasks: services.NewTasksService(services.NewTranslationService(client), nil)}
	api.HandleTranslateBatchTasks(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Results []services.BatchTranslateResult `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("results: %+v", response)
	}
	if response.Results[0].Success || response.Results[0].TranslatedText != "" {
		t.Errorf("stale caller accepted current translation: %+v", response.Results[0])
	}
	if !response.Results[1].Success || response.Results[1].TranslatedText != "Stable translation" {
		t.Errorf("valid item lost: %+v", response.Results[1])
	}
}

func TestTranslateBatchRejectsLateAIAndPreservesPartialErrors(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	email := testutil.RandomEmail("http-revision")
	ids := make([]store.MessageID, 3)
	for i, title := range []string{"A", "Stable", "Failed"} {
		result, err := store.GetDB().ExecContext(ctx, "INSERT INTO messages(user_email, task, source, source_ts) VALUES(?, ?, 'slack', ?)", email, title, testutil.RandomTS(title))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		ids[i] = store.MessageID(id)
	}
	client, err := ai.NewAIClient(ai.ProviderConfig{DeepSeekAPIKey: "test-key", DeepSeekTranslationModel: "test-model", DeepSeekBaseURL: testutil.NewChatCompletionServer(t, func(r *http.Request) (string, error) {
		if _, err := store.GetDB().ExecContext(ctx, "UPDATE messages SET task='B' WHERE id=?", ids[0]); err != nil {
			return "", err
		}
		if err := store.SaveTaskTranslationsBulk(ctx, "ko", map[store.MessageID]string{ids[0]: "B translation"}); err != nil {
			return "", err
		}
		results, _ := json.Marshal([]ai.TranslationResult{{MessageID: ids[0], Text: "late A"}, {MessageID: ids[1], Text: "stable translation"}, {MessageID: ids[2], Error: "item failed"}})
		return string(results), nil
	}),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(struct {
		IDs  []store.MessageID `json:"task_ids"`
		Lang string            `json:"lang"`
	}{ids, "ko"})
	req := httptest.NewRequest("POST", "/api/tasks/translate-batch", bytes.NewReader(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api := &API{Tasks: services.NewTasksService(services.NewTranslationService(client), nil)}
	api.HandleTranslateBatchTasks(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Results []services.BatchTranslateResult `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 3 {
		t.Fatalf("results: %+v", response)
	}
	if response.Results[0].Success || response.Results[0].TranslatedText != "" {
		t.Errorf("stale result succeeded: %+v", response.Results[0])
	}
	if !response.Results[1].Success {
		t.Errorf("valid partial lost: %+v", response.Results[1])
	}
	if response.Results[2].Success || response.Results[2].Error != "item failed" {
		t.Errorf("item error lost: %+v", response.Results[2])
	}
	cached, err := store.GetTaskTranslationsBatch(ctx, ids, "ko")
	if err != nil || cached[ids[0]] != "B translation" {
		t.Fatalf("new title translation overwritten: %v, %v", cached, err)
	}
}

func TestTranslateBatchRejectsForeignCachedAndUncachedMessages(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	email := testutil.RandomEmail("request-owner")
	ids := make([]store.MessageID, 3)
	for i, owner := range []string{email, "foreign@example.com", "foreign@example.com"} {
		result, err := store.GetDB().ExecContext(ctx, "INSERT INTO messages(user_email, task, source, source_ts) VALUES(?, 'Private title', 'slack', ?)", owner, testutil.RandomTS(fmt.Sprint(i)))
		if err != nil {
			t.Fatal(err)
		}
		rawID, _ := result.LastInsertId()
		ids[i] = store.MessageID(rawID)
	}
	if err := store.SaveTaskTranslationsBulk(ctx, "ko", map[store.MessageID]string{ids[0]: "Own translation", ids[1]: "Foreign translation"}); err != nil {
		t.Fatal(err)
	}
	client, err := ai.NewAIClient(ai.ProviderConfig{DeepSeekAPIKey: "test-key", DeepSeekTranslationModel: "test-model", DeepSeekBaseURL: testutil.NewChatCompletionServer(t, func(r *http.Request) (string, error) {
		t.Error("foreign uncached message must not reach AI")
		return "[]", nil
	}),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(struct {
		IDs  []store.MessageID `json:"task_ids"`
		Lang string            `json:"lang"`
	}{ids, "ko"})
	req := httptest.NewRequest("POST", "/api/tasks/translate-batch", bytes.NewReader(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api := &API{Tasks: services.NewTasksService(services.NewTranslationService(client), nil)}
	api.HandleTranslateBatchTasks(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Results []services.BatchTranslateResult `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 3 {
		t.Fatalf("results: %+v", response)
	}
	if !response.Results[0].Success || response.Results[0].TranslatedText != "Own translation" {
		t.Errorf("own translation lost: %+v", response.Results[0])
	}
	for _, item := range response.Results[1:] {
		if item.Success || item.TranslatedText != "" {
			t.Errorf("foreign translation leaked: %+v", item)
		}
	}
}
