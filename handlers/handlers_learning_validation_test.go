package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func setupCorrectionValidationTask(t *testing.T, originalText string) (string, store.MessageID) {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup test database: %v", err)
	}
	t.Cleanup(cleanup)
	email := testutil.RandomEmail("correction-validation")
	if _, err := store.GetOrCreateUser(t.Context(), email, "Owner", ""); err != nil {
		t.Fatalf("create task owner: %v", err)
	}
	_, id, err := store.SaveMessage(t.Context(), store.GetDB(), store.ConsolidatedMessage{
		UserEmail: email, Source: "slack", Room: "Learning room", SourceTS: "correction-source",
		Task: "Original title", Assignee: "Owner", Requester: "Sender", Category: "TASK", OriginalText: originalText,
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return email, id
}

func requestMessageEditForTest(t *testing.T, handler http.HandlerFunc, email string, body patchMessageDetailsRequest) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode correction: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/messages/details", bytes.NewReader(encoded))
	req = req.WithContext(WithMockUser(req.Context(), email))
	response := httptest.NewRecorder()
	handler(response, req)
	return response
}

func TestPatchMessageDetails_RejectsBlankTaskAndPreservesExistingTask(t *testing.T) {
	for _, tc := range []struct {
		name string
		task string
	}{
		{name: "empty", task: ""},
		{name: "ascii_whitespace", task: " \t\r\n "},
		{name: "unicode_whitespace", task: "\u2003\u00a0\t\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email, id := setupCorrectionValidationTask(t, "")
			assignee := "Changed assignee"
			response := requestMessageEditForTest(t, (&API{}).HandlePatchMessageDetails, email, patchMessageDetailsRequest{ID: id, Task: &tc.task, Assignee: &assignee})
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d body=%s; want 400", response.Code, response.Body.String())
			}
			var payload struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Errorf("decode validation response: %v", err)
			} else if payload.Error != "task is required" {
				t.Errorf("error = %q; want task is required", payload.Error)
			}
			assertCorrectionStoredAndListed(t, email, id, "Original title", "Owner")
		})
	}
}

func TestUpdateTask_UsesSameTitleValidationContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		task       string
		wantStatus int
		wantTask   string
	}{
		{name: "empty", task: "", wantStatus: http.StatusBadRequest, wantTask: "Original title"},
		{name: "ascii_whitespace", task: " \t\r\n ", wantStatus: http.StatusBadRequest, wantTask: "Original title"},
		{name: "unicode_whitespace", task: "\u2003\u00a0\t\n", wantStatus: http.StatusBadRequest, wantTask: "Original title"},
		{name: "valid", task: "Revised title", wantStatus: http.StatusOK, wantTask: "Revised title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email, id := setupCorrectionValidationTask(t, "")
			response := requestMessageEditForTest(t, (&API{}).HandleUpdateTask, email, patchMessageDetailsRequest{ID: id, Task: &tc.task})
			if response.Code != tc.wantStatus {
				t.Errorf("status = %d body=%s; want %d", response.Code, response.Body.String(), tc.wantStatus)
			}
			if tc.wantStatus == http.StatusBadRequest {
				var payload struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Errorf("decode validation response: %v", err)
				} else if payload.Error != "task is required" {
					t.Errorf("error = %q; want task is required", payload.Error)
				}
			}
			assertCorrectionStoredAndListed(t, email, id, tc.wantTask, "Owner")
		})
	}
}

func TestPatchMessageDetails_RejectsBlankTaskBeforeMessageLookup(t *testing.T) {
	email, id := setupCorrectionValidationTask(t, "")
	blank := "\u2003\t"
	response := requestMessageEditForTest(t, (&API{}).HandlePatchMessageDetails, email, patchMessageDetailsRequest{ID: id + 1000, Task: &blank})
	if response.Code != http.StatusBadRequest {
		t.Errorf("status = %d body=%s; want 400 before missing-message lookup", response.Code, response.Body.String())
	}
}

func TestPatchMessageDetails_ValidAndOmittedTaskKeepLearning(t *testing.T) {
	newTitle, newAssignee := "Revised title", "Delegate"
	for _, tc := range []struct {
		name         string
		task         *string
		assignee     *string
		wantTask     string
		wantAssignee string
	}{
		{name: "valid_task", task: &newTitle, wantTask: newTitle, wantAssignee: "Owner"},
		{name: "omitted_task_with_other_field", assignee: &newAssignee, wantTask: "Original title", wantAssignee: newAssignee},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email, id := setupCorrectionValidationTask(t, "Please handle the original task")
			response := requestMessageEditForTest(t, (&API{}).HandlePatchMessageDetails, email, patchMessageDetailsRequest{ID: id, Task: tc.task, Assignee: tc.assignee})
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s; want 200", response.Code, response.Body.String())
			}
			assertCorrectionStoredAndListed(t, email, id, tc.wantTask, tc.wantAssignee)
			assertCorrectionLearningExample(t, email, tc.wantTask, tc.wantAssignee)
		})
	}
}

func assertCorrectionStoredAndListed(t *testing.T, email string, id store.MessageID, wantTask, wantAssignee string) {
	t.Helper()
	var task, assignee string
	if err := store.GetDB().QueryRowContext(t.Context(), "SELECT task, assignee FROM messages WHERE id = ? AND user_email = ?", id, email).Scan(&task, &assignee); err != nil {
		t.Fatalf("read stored correction: %v", err)
	}
	if task != wantTask || assignee != wantAssignee {
		t.Errorf("stored task=%q assignee=%q; want task=%q assignee=%q", task, assignee, wantTask, wantAssignee)
	}
	if err := store.RefreshCache(t.Context(), email); err != nil {
		t.Fatalf("refresh message listing: %v", err)
	}
	response := httptest.NewRecorder()
	(&API{}).HandleGetMessages(response, NewMockRequest(http.MethodGet, "/api/messages", email))
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s; want 200", response.Code, response.Body.String())
	}
	var listed store.CategorizedMessages
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode message listing: %v", err)
	}
	for _, group := range [][]store.ConsolidatedMessage{listed.Inbox, listed.Delegated, listed.Reference} {
		for _, msg := range group {
			if msg.ID == id && msg.Task == wantTask && msg.Assignee == wantAssignee {
				return
			}
		}
	}
	t.Errorf("corrected task %d missing or changed in active listing: %s", id, response.Body.String())
}

func assertCorrectionLearningExample(t *testing.T, email, wantTask, wantAssignee string) {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		examples, err := db.New(store.GetDB()).ListLearnedExamples(t.Context(), db.ListLearnedExamplesParams{UserEmail: email, Limit: 10})
		if err != nil {
			t.Fatalf("list learned examples: %v", err)
		}
		if len(examples) > 0 {
			var expected []struct {
				Task     string `json:"task"`
				Assignee string `json:"assignee"`
			}
			if err := json.Unmarshal([]byte(examples[0].Expected), &expected); err != nil {
				t.Fatalf("decode learned correction: %v", err)
			}
			if examples[0].Origin != "edit_confirm" || len(expected) != 1 || expected[0].Task != wantTask || expected[0].Assignee != wantAssignee {
				t.Errorf("learned correction does not match saved fields: %+v expected=%+v", examples[0], expected)
			}
			return
		}
		select {
		case <-timeout.C:
			t.Fatal("correction learning did not persist an edit_confirm example")
		case <-ticker.C:
		}
	}
}
