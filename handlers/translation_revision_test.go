package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
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
	api := &API{Tasks: services.NewTasksService(services.NewTranslationService(nil), nil)}
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
