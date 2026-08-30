package core

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
			expected: 9, // Only 9 total in DefaultFewShots
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

// TestSelectFewShotsForSourceAffinity verifies a learned shot tagged with the requesting
// channel outranks an unrelated, higher-keyword-overlap seed shot.
func TestSelectFewShotsForSourceAffinity(t *testing.T) {
	unrelatedSeed := FewShot{Input: "deploy the app to slack channel", Expected: "seed"}
	matchingLearned := FewShot{Input: "some unrelated wording", Expected: "learned", Source: "whatsapp"}
	examples := []FewShot{unrelatedSeed, matchingLearned}

	result := SelectFewShotsForSource("deploy", "whatsapp", examples, 1)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].Expected != "learned" {
		t.Errorf("expected source-matching learned shot to outrank seed, got %+v", result[0])
	}
}

// TestSelectFewShotsForSourceNegativeExample verifies a negative example (Expected == "[]")
// is a normal pool member, selectable like any other shot -- no special-casing suppresses it.
func TestSelectFewShotsForSourceNegativeExample(t *testing.T) {
	negative := FewShot{Input: "[ID:s1] noise message with no task", Expected: "[]", Source: "slack"}
	examples := []FewShot{negative}

	result := SelectFewShotsForSource("noise message", "slack", examples, 1)
	if len(result) != 1 || result[0].Expected != "[]" {
		t.Errorf("expected negative example to be selectable, got %+v", result)
	}
}

// TestFewShotLimitBump verifies SelectFewShotsForSource honors a raised limit so a learned
// shot can appear alongside both seed shots instead of evicting one (analyzers.go bumps
// limit 2->3 only when at least one learned shot exists in the pool).
func TestFewShotLimitBump(t *testing.T) {
	seeds := GetDefaultFewShots()[:2]
	learned := FewShot{Input: "learned example text", Expected: "learned", Source: "slack"}
	pool := append(append([]FewShot{}, seeds...), learned)

	withoutBump := SelectFewShotsForSource("payload", "slack", pool, 2)
	if len(withoutBump) != 2 {
		t.Fatalf("expected 2 results at limit 2, got %d", len(withoutBump))
	}

	withBump := SelectFewShotsForSource("payload", "slack", pool, 3)
	if len(withBump) != 3 {
		t.Fatalf("expected 3 results at limit 3, got %d", len(withBump))
	}
}
