package services

import (
	"strings"
	"unicode"
)

// ftsCandidateTokens splits text on non-letter/digit runes, keeps tokens >= 3 runes
// (trigram tokenizer minimum), dedupes case-insensitively, and caps at max.
func ftsCandidateTokens(text string, max int) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, max)
	for _, f := range fields {
		if len([]rune(f)) < 3 {
			continue
		}
		key := strings.ToLower(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
		if len(out) == max {
			break
		}
	}
	return out
}

// countTokenHits reports how many distinct tokens appear as a case-insensitive
// substring of haystack. Shared by titleTokenOverlap and topicalOverlap so both
// gates apply the same case-folding and matching semantics.
func countTokenHits(tokens []string, haystack string) int {
	lower := strings.ToLower(haystack)
	hits := 0
	for _, t := range tokens {
		if strings.Contains(lower, strings.ToLower(t)) {
			hits++
		}
	}
	return hits
}
