package store

import "testing"

func TestCalculateSimilarity(t *testing.T) {
	tests := []struct {
		a, b     string
		minScore float64
	}{
		{"Share the meeting invite", "Please share the calendar invite", 0.65},
		{"Check server status", "check SERVER status!!!", 0.95},
		{"Review the draft", "Draft for review", 0.50},
		{"Hello world", "Completely different", 0.0},
	}

	for _, tt := range tests {
		score := CalculateSimilarity(tt.a, tt.b)
		if score < tt.minScore {
			t.Errorf("Similarity(%q, %q) = %f; want at least %f", tt.a, tt.b, score, tt.minScore)
		}
	}
}
