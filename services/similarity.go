package services

import (
	"regexp"
	"strings"
)

var puncRegex = regexp.MustCompile(`[^\p{L}\p{N}\s]`)

// CalculateSimilarity returns a score between 0.0 and 1.0 for two strings.
// Uses a simplified Jaro distance implementation for 40-line constraint.
func CalculateSimilarity(a, b string) float64 {
	s1 := preprocessString(a)
	s2 := preprocessString(b)
	if s1 == s2 { return 1.0 }
	if s1 == "" || s2 == "" { return 0.0 }

	return jaroDistance(s1, s2)
}

func preprocessString(s string) string {
	s = strings.ToLower(s)
	s = puncRegex.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

func jaroDistance(s1, s2 string) float64 {
	l1, l2 := len(s1), len(s2)
	matchDist := (max(l1, l2) / 2) - 1
	if matchDist < 0 { matchDist = 0 }

	m1, m2 := make([]bool, l1), make([]bool, l2)
	matches := 0
	for i := 0; i < l1; i++ {
		start, end := max(0, i-matchDist), min(i+matchDist+1, l2)
		for j := start; j < end; j++ {
			if !m2[j] && s1[i] == s2[j] {
				m1[i], m2[j], matches = true, true, matches+1
				break
			}
		}
	}
	if matches == 0 { return 0.0 }
	return jaroWinklerScore(s1, s2, m1, m2, matches)
}

func jaroWinklerScore(s1, s2 string, m1, m2 []bool, matches int) float64 {
	l1, l2 := len(s1), len(s2)
	transpositions, k := 0, 0
	for i := 0; i < l1; i++ {
		if !m1[i] { continue }
		for !m2[k] { k++ }
		if s1[i] != s2[k] { transpositions++ }
		k++
	}
	m := float64(matches)
	jaro := (m/float64(l1) + m/float64(l2) + (m-float64(transpositions/2))/m) / 3.0
	prefix := 0
	for i := 0; i < min(6, min(l1, l2)); i++ {
		if s1[i] != s2[i] { break }
		prefix++
	}
	return jaro + (float64(prefix) * 0.15 * (1.0 - jaro))
}

func max(a, b int) int { if a > b { return a }; return b }
func min(a, b int) int { if a < b { return a }; return b }
