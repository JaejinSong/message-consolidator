package ai

import (
	"encoding/base64"
	"testing"
)

func TestDecodeBase64URL(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
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

func TestSanitizeJSON(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeJSON(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeJSON() = %v, want %v (Input: %s)", got, tt.expected, tt.input)
			}
		})
	}
}

func TestUnmarshalAnalyze(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			got, err := unmarshalAnalyze(tt.cleanJSON, tt.cleanJSON)
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

func TestUnmarshalTranslate(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
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
