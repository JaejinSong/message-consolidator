package services

import (
	"context"
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
)

const (
	manualMatchWindow    = 7 * 24 * time.Hour
	manualMatchMinTokens = 3
	manualMatchScanLimit = 211 // Why: bound the per-add scan; prime per project convention.
)

// linkDeletedTaskToFilterScore labels the passed filter row behind a deleted undone task.
// Why: best effort -- a labeling failure must never affect the deletion learning path.
func linkDeletedTaskToFilterScore(ctx context.Context, userEmail string, m store.ConsolidatedMessage) {
	if m.SourceTS == "" {
		return
	}
	if err := store.LinkFilterOutcomeBySourceTS(ctx, userEmail, m.Source, m.Room, m.SourceTS, store.FilterOutcomeDeletedNoise); err != nil {
		logger.Warnf("[FILTER] link deleted task msg=%d: %v", m.ID, err)
	}
}

// matchManualAddToFilterDrop labels the best-overlapping recent unlabeled noise drop as a
// false drop. Why: a manually added task that mirrors a dropped message approximates a missed task.
func matchManualAddToFilterDrop(ctx context.Context, userEmail, taskText, originalText string) {
	tokens := tokenSet(suppressSignature(taskText + " " + originalText))
	if len(tokens) < manualMatchMinTokens {
		return
	}
	drops, err := store.ListFilterDrops(ctx, userEmail, time.Now().Add(-manualMatchWindow), manualMatchScanLimit)
	if err != nil {
		logger.Warnf("[FILTER] list drops for manual match: %v", err)
		return
	}
	var bestID int64
	best := manualMatchMinTokens - 1
	for _, d := range drops {
		if d.Outcome != "" {
			continue
		}
		if n := sharedTokenCount(tokens, suppressSignature(d.Text)); n > best {
			best, bestID = n, d.ID
		}
	}
	if bestID == 0 {
		return
	}
	if err := store.SetFilterOutcome(ctx, userEmail, bestID, store.FilterOutcomeManualMatch); err != nil {
		logger.Warnf("[FILTER] label manual match id=%d: %v", bestID, err)
	}
}

func tokenSet(tokens []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		set[t] = struct{}{}
	}
	return set
}

func sharedTokenCount(set map[string]struct{}, tokens []string) int {
	n := 0
	for _, t := range tokens {
		if _, ok := set[t]; ok {
			n++
		}
	}
	return n
}
