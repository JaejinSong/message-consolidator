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

// Why: guards the textbook Jaro-Winkler prefix bonus (cap 4, scale 0.1) against
// regressing into a shared-first-word collision (see similarity.go jaroWinkler).
func TestCalculateSimilarity_PrefixBonusBounds(t *testing.T) {
	const dedupThreshold = 0.85

	tests := []struct {
		name      string
		a, b      string
		wantScore float64
		exact     bool
		belowMax  bool
	}{
		{
			name:      "identical strings score 1.0",
			a:         "Update the API documentation",
			b:         "Update the API documentation",
			wantScore: 1.0,
			exact:     true,
		},
		{
			name: "typo-level near duplicate stays above dedup threshold",
			a:    "Update the API documentation",
			b:    "Update the API documentaton",
		},
		{
			name:     "shared opening word with unrelated content stays below dedup threshold",
			a:        "Arrange dinner restaurant with 3 sales from GEC",
			b:        "Arrange lunch at One Bangkok",
			belowMax: true,
		},
		{
			name:     "shared opening word with unrelated topic stays below dedup threshold",
			a:        "Clarify Aurora serverless pricing",
			b:        "Clarify hook_method_patterns usage",
			belowMax: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := CalculateSimilarity(tt.a, tt.b)
			switch {
			case tt.exact:
				if score != tt.wantScore {
					t.Errorf("Similarity(%q, %q) = %f; want exactly %f", tt.a, tt.b, score, tt.wantScore)
				}
			case tt.belowMax:
				if score >= dedupThreshold {
					t.Errorf("Similarity(%q, %q) = %f; want below %f", tt.a, tt.b, score, dedupThreshold)
				}
			default:
				if score < dedupThreshold {
					t.Errorf("Similarity(%q, %q) = %f; want at least %f", tt.a, tt.b, score, dedupThreshold)
				}
			}
		})
	}
}
