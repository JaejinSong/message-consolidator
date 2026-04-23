package store

import "testing"

func TestCategoryOrDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"", "TASK"},
		{"TASK", "TASK"},
		{"QUERY", "QUERY"},
		{"PROMISE", "PROMISE"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run("input="+tt.input, func(t *testing.T) {
			t.Parallel()
			if got := categoryOrDefault(tt.input); got != tt.expected {
				t.Errorf("categoryOrDefault(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
