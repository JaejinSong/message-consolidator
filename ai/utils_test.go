package ai

import (
	"encoding/base64"
	"message-consolidator/store"
	"testing"
)

func TestDecodeBase64URL(t *testing.T) {
	t.Parallel()
	//Why: Uses a sample string with special characters (? and !) to verify robust URL-safe Base64 decoding.
	originalText := "hello? world! & testing~"

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "1. URL-safe encoding with padding",
			input:    base64.URLEncoding.EncodeToString([]byte(originalText)),
			expected: originalText,
			wantErr:  false,
		},
		{
			name:     "2. URL-safe encoding without padding (Fallback)",
			input:    base64.RawURLEncoding.EncodeToString([]byte(originalText)),
			expected: originalText,
			wantErr:  false,
		},
		{
			name:     "3. Standard encoding with padding (Fallback)",
			input:    base64.StdEncoding.EncodeToString([]byte(originalText)),
			expected: originalText,
			wantErr:  false,
		},
		{
			name:     "4. Standard encoding without padding (Fallback)",
			input:    base64.RawStdEncoding.EncodeToString([]byte(originalText)),
			expected: originalText,
			wantErr:  false,
		},
		{
			name:     "5. Invalid base64 data",
			input:    "invalid_base64_data!@#$",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		tt := tt // Closure capture
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeBase64URL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeBase64URL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("DecodeBase64URL() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCleanMarkdownText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Markdown JSON block",
			input:    "```json\n{\"id\": 1}\n```",
			expected: "{\"id\": 1}",
		},
		{
			name:     "Markdown text block",
			input:    "```text\nSome text\n```",
			expected: "Some text",
		},
		{
			name:     "Generic markdown block",
			input:    "```\nRaw content\n```",
			expected: "Raw content",
		},
		{
			name:     "Markdown markdown block",
			input:    "```markdown\n# Hello\n```",
			expected: "# Hello",
		},
		{
			name:     "No markdown blocks",
			input:    "Pure text",
			expected: "Pure text",
		},
		{
			name:     "Trailing and leading spaces",
			input:    "  ```json\n{\"id\": 1}\n```  ",
			expected: "{\"id\": 1}",
		},
		{
			name:     "Multiple lines",
			input:    "```json\n{\n  \"id\": 1\n}\n```",
			expected: "{\n  \"id\": 1\n}",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CleanMarkdownText(tt.input)
			if got != tt.expected {
				t.Errorf("CleanMarkdownText() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSanitizeJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Clean JSON array",
			input:    `[{"id": 1}]`,
			expected: `[{"id": 1}]`,
		},
		{
			name:     "Markdown JSON block",
			input:    "```json\n[{\"id\": 1}]\n```",
			expected: `[{"id": 1}]`,
		},
		{
			name:     "Text before and after JSON",
			input:    `Here is the data: [{"id": 1}] hope it helps!`,
			expected: `[{"id": 1}]`,
		},
		{
			name:     "Truncated JSON array (Self-Healing)",
			input:    `[{"id": 1}, {"id": 2}`,
			expected: `[{"id": 1}, {"id": 2}]`,
		},
		{
			name:     "Markdown with truncated array",
			input:    "```json\n[{\"id\": 1}, {\"id\": 2}\n",
			expected: `[{"id": 1}, {"id": 2}]`,
		},
		{
			name:     "Single JSON object (No repair needed)",
			input:    `{"status": "ok"}`,
			expected: `{"status": "ok"}`,
		},
		{
			name:     "Empty or broken input",
			input:    `[`,
			expected: "",
		},
		{
			name:     "Nested structures",
			input:    `Results: {"data": [1, 2, 3]}`,
			expected: `{"data": [1, 2, 3]}`,
		},
	}

	for _, tt := range tests {
		tt := tt // Closure capture
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeJSON(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeJSON() = %v, want %v (Input: %s)", got, tt.expected, tt.input)
			}
		})
	}
}

func TestUnmarshalAnalyze(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cleanJSON string
		expected  int
		wantErr   bool
	}{
		{"Array format", `[{"task": "Task 1"}]`, 1, false},
		{"Object format (tasks)", `{"tasks": [{"task": "Task 2"}]}`, 1, false},
		{"Object format (items)", `{"items": [{"task": "Task 3"}]}`, 1, false},
		{"Empty array", `[]`, 0, false},
	}

	for _, tt := range tests {
		tt := tt // Closure capture
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := unmarshalAnalyze(tt.cleanJSON, tt.cleanJSON, "", 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalAnalyze() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.expected {
				t.Errorf("unmarshalAnalyze() len = %v, want %v", len(got), tt.expected)
			}
		})
	}
}

func TestMapFlexToTodo_IdentityNormalization(t *testing.T) {
	t.Parallel()
	const userID store.UserID = 42
	const userEmail = "jj@example.com"
	ptr := func(i store.UserID) *store.UserID { return &i }

	tests := []struct {
		name                    string
		item                    flexItem
		wantAssignee            string
		wantRequesterCanonical  string
		wantSubtaskAssigneeName string
	}{
		{
			name:         "assignee_id matches → userEmail",
			item:         flexItem{Task: "t", AssigneeID: ptr(userID), Assignee: "Jaejin Song (JJ)"},
			wantAssignee: userEmail,
		},
		{
			name:         "assignee_id mismatch → name unchanged",
			item:         flexItem{Task: "t", AssigneeID: ptr(99), Assignee: "Hady"},
			wantAssignee: "Hady",
		},
		{
			name:         "assignee_id nil → name unchanged",
			item:         flexItem{Task: "t", Assignee: "Hady"},
			wantAssignee: "Hady",
		},
		{
			name:                   "requester_id matches → RequesterCanonical set",
			item:                   flexItem{Task: "t", RequesterID: ptr(userID), Requester: "Jaejin Song (JJ)"},
			wantRequesterCanonical: userEmail,
		},
		{
			name:                   "requester_id mismatch → RequesterCanonical empty",
			item:                   flexItem{Task: "t", RequesterID: ptr(99), Requester: "Hady"},
			wantRequesterCanonical: "",
		},
		{
			name: "subtask assignee_id matches → userEmail",
			item: flexItem{Task: "t", Subtasks: []flexSubtask{
				{Task: "sub", AssigneeID: ptr(userID), AssigneeName: "Jaejin Song (JJ)"},
			}},
			wantSubtaskAssigneeName: userEmail,
		},
		{
			name: "subtask assignee_id mismatch → name unchanged",
			item: flexItem{Task: "t", Subtasks: []flexSubtask{
				{Task: "sub", AssigneeID: ptr(99), AssigneeName: "Hady"},
			}},
			wantSubtaskAssigneeName: "Hady",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapFlexToTodo(tt.item, userID, userEmail)
			if tt.wantAssignee != "" && got.Assignee != tt.wantAssignee {
				t.Errorf("Assignee = %q, want %q", got.Assignee, tt.wantAssignee)
			}
			if got.RequesterCanonical != tt.wantRequesterCanonical {
				t.Errorf("RequesterCanonical = %q, want %q", got.RequesterCanonical, tt.wantRequesterCanonical)
			}
			if tt.wantSubtaskAssigneeName != "" && len(got.Subtasks) > 0 {
				if got.Subtasks[0].AssigneeName != tt.wantSubtaskAssigneeName {
					t.Errorf("Subtask[0].AssigneeName = %q, want %q", got.Subtasks[0].AssigneeName, tt.wantSubtaskAssigneeName)
				}
			}
		})
	}
}

func TestMapFlexToTodo_StatusMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		item       flexItem
		wantStatus string
		wantState  string
	}{
		{"status=resolve is mapped to TodoItem.Status", flexItem{Task: "t", Status: "resolve"}, "resolve", ""},
		{"status=new is mapped to TodoItem.Status", flexItem{Task: "t", Status: "new"}, "new", ""},
		{"state field still populates TodoItem.State", flexItem{Task: "t", State: "update"}, "", "update"},
		{"empty status stays empty", flexItem{Task: "t"}, "", ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapFlexToTodo(tt.item, 0, "")
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
			}
		})
	}
}

func TestUnmarshalAnalyze_StatusKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		json       string
		wantStatus string
		wantState  string
	}{
		{
			name:       "new_extraction format: status key → TodoItem.Status",
			json:       `[{"task": "Follow up", "status": "resolve", "category": "PROMISE"}]`,
			wantStatus: "resolve",
			wantState:  "",
		},
		{
			name:      "gmail format: state key → TodoItem.State",
			json:      `[{"task": "Follow up", "state": "resolve", "category": "TASK"}]`,
			wantState: "resolve",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			items, err := unmarshalAnalyze(tt.json, tt.json, "", 0)
			if err != nil {
				t.Fatalf("unmarshalAnalyze() error = %v", err)
			}
			if len(items) == 0 {
				t.Fatal("expected at least 1 item")
			}
			if items[0].Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", items[0].Status, tt.wantStatus)
			}
			if items[0].State != tt.wantState {
				t.Errorf("State = %q, want %q", items[0].State, tt.wantState)
			}
		})
	}
}

func TestUnmarshalTranslate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cleanJSON string
		expected  int
		wantErr   bool
	}{
		{"Array format", `[{"id": 1, "text": "Task 1"}]`, 1, false},
		{"Object format (translations)", `{"translations": [{"id": 2, "text": "Task 2"}]}`, 1, false},
		{"Invalid format", `{"wrong": 123}`, 0, true},
	}

	for _, tt := range tests {
		tt := tt // Closure capture
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := unmarshalTranslate(tt.cleanJSON, tt.cleanJSON, "en")
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalTranslate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.expected {
				t.Errorf("unmarshalTranslate() len = %v, want %v", len(got), tt.expected)
			}
		})
	}
}
