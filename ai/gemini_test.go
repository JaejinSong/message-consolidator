package ai

import (
	"testing"
)

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
