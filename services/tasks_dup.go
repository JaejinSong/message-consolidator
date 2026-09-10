package services

import (
	"strings"
	"unicode"

	"message-consolidator/store"
)

const (
	// minDuplicateShared is the floor on shared content tokens. Why: two- or three-word
	// titles can reach a high ratio by accident ("Review NDA" vs "Review PO").
	minDuplicateShared = 3
	// minDuplicateRatio is the share of the shorter title's content tokens that must be
	// common to both. Why: an absolute count has no discriminating power in this domain --
	// "...quotation to Customer Alpha..." and "...to Customer Beta..." share 8 tokens,
	// exactly as many as a genuine rewrite (measured 2026-09-10). A ratio plus the
	// proper-noun veto below is what separates them.
	minDuplicateRatio = 0.7
)

// duplicateStopwords are function words long enough to clear the 3-rune token floor
// while carrying no topic, so they must not inflate the overlap of two generically
// worded English titles.
var duplicateStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "via": true, "with": true, "from": true,
	"into": true, "onto": true, "out": true, "all": true, "any": true, "are": true,
	"was": true, "has": true, "have": true, "been": true, "will": true, "can": true,
	"not": true, "its": true, "our": true, "their": true, "this": true, "that": true,
	"these": true, "those": true, "than": true, "then": true, "when": true, "while": true,
	"about": true, "after": true, "before": true, "during": true, "over": true,
	"under": true, "per": true, "upon": true, "his": true, "her": true, "you": true,
	"your": true, "please": true,
}

// findDuplicate returns the active task that item restates, or nil. It is deliberately
// separate from findMatch: this path crosses categories, so callers must treat its
// result as link-only and never let it hard-close a task.
//
// Picks the highest-scoring candidate rather than the first. Why: GetActiveTasksForContext
// orders by assigned_at DESC, so a first-match rule would attach a duplicate to whichever
// qualifying task happens to be newest.
func (s *TasksService) findDuplicate(room string, item store.TodoItem, active []store.ConsolidatedMessage) *store.ConsolidatedMessage {
	if strings.TrimSpace(item.Task) == "" {
		return nil
	}
	var best *store.ConsolidatedMessage
	bestRatio := 0.0
	for i := range active {
		m := &active[i]
		if m.Room != room || isArchivedCandidate(m) {
			continue
		}
		if item.ThreadID != "" && m.ThreadID != "" && item.ThreadID != m.ThreadID {
			continue
		}
		ov := measureTitleOverlap(item.Task, m.Task)
		if ov.shared < minDuplicateShared || ov.coverage < minDuplicateRatio {
			continue
		}
		if hasEntityConflict(item.Task, m.Task) {
			continue
		}
		// Why: coverage normalizes by the shorter title, so a subset scores the same 1.0
		// as an exact match; Jaccard separates them and picks the closest candidate.
		if ov.jaccard > bestRatio {
			best, bestRatio = m, ov.jaccard
		}
	}
	return best
}

// titleOverlap carries the two measures of title similarity: coverage gates a candidate,
// jaccard ranks the ones that qualify.
type titleOverlap struct {
	shared   int
	coverage float64
	jaccard  float64
}

// measureTitleOverlap compares the content-token sets of two titles. coverage is the
// shared count over the shorter title's own tokens (so a rewrite that only adds detail
// still qualifies); jaccard is the shared count over the union.
func measureTitleOverlap(a, b string) titleOverlap {
	aTokens, bTokens := contentTokens(a), contentTokens(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return titleOverlap{}
	}
	shared := 0
	for token := range aTokens {
		if bTokens[token] {
			shared++
		}
	}
	shorter := len(aTokens)
	if len(bTokens) < shorter {
		shorter = len(bTokens)
	}
	union := len(aTokens) + len(bTokens) - shared
	return titleOverlap{
		shared:   shared,
		coverage: float64(shared) / float64(shorter),
		jaccard:  float64(shared) / float64(union),
	}
}

// hasEntityConflict reports whether each title names a proper noun the other one lacks.
// Why: that is what distinguishes "same request, reworded" from "same template, different
// customer". A one-sided difference is not a conflict -- it is usually the longer form of
// the same name ("TGIA Insurance CIO Forum" vs "TGIA CIO Forum").
func hasEntityConflict(a, b string) bool {
	aNames, bNames := properNouns(a), properNouns(b)
	return hasExclusive(aNames, bNames) && hasExclusive(bNames, aNames)
}

func hasExclusive(names, other map[string]bool) bool {
	for name := range names {
		if !other[name] {
			return true
		}
	}
	return false
}

// properNouns collects lowercased tokens that appear capitalized away from the title's
// first word, which in an English title marks a named entity rather than a common noun.
// Digit-only tokens are excluded so a shared year cannot act as a name.
func properNouns(title string) map[string]bool {
	out := make(map[string]bool)
	for i, raw := range splitContentFields(title) {
		if i == 0 {
			continue // Why: the first word of a title is always capitalized.
		}
		runes := []rune(raw)
		if len(runes) < 2 || !unicode.IsUpper(runes[0]) {
			continue
		}
		key := strings.ToLower(raw)
		if duplicateStopwords[key] {
			continue
		}
		out[key] = true
	}
	return out
}

// contentTokens is the lowercased, stopword-filtered token set used for overlap.
func contentTokens(s string) map[string]bool {
	out := make(map[string]bool)
	for _, f := range splitContentFields(s) {
		if len([]rune(f)) < 3 {
			continue
		}
		key := strings.ToLower(f)
		if duplicateStopwords[key] {
			continue
		}
		out[key] = true
	}
	return out
}

func splitContentFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
