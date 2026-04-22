package ai

import (
	"testing"
)

func TestSelectFewShots(t *testing.T) {
	allShots := GetDefaultFewShots()

	tests := []struct {
		name     string
		payload  string
		limit    int
		expected int // Expected number of results
	}{
		{
			name:     "Empty payload",
			payload:  "",
			limit:    2,
			expected: 2,
		},
		{
			name:     "Slack matching",
			payload:  "[ID:Slack_99] Deploy server",
			limit:    1,
			expected: 1,
		},
		{
			name:     "No limit",
			payload:  "general task",
			limit:    10,
			expected: 6, // Only 6 total in DefaultFewShots
		},
		{
			name:     "Zero or negative limit",
			payload:  "something",
			limit:    0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectFewShots(tt.payload, allShots, tt.limit)
			if len(result) != tt.expected {
				t.Errorf("Expected %d results, got %d", tt.expected, len(result))
			}
		})
	}
}
