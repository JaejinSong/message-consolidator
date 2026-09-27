package services

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
)

// Prime intervals per project convention.
const (
	// pastEventLookbackDays bounds how far back a passed event date is still worth
	// nudging on -- older misses are better surfaced by the stalled/exclusion loops.
	pastEventLookbackDays = 29
	// pastEventScanLimit caps rows scanned per user per tick.
	pastEventScanLimit = 97
)

// eventTaskPattern matches whole-word event verbs/nouns in an extracted task title.
// Why: word-boundary regex avoids false positives like "Callback URL fix" (contains
// "call" as a substring, not the standalone word) while still catching titles like
// "Join Mesiniaga session at 11am".
var eventTaskPattern = regexp.MustCompile(`(?i)\b(meet|meeting|join|attend|session|call|visit|onsite|demo|presentation|webinar|workshop|discussion|kickoff|sync)\b`)

// isEventTask reports whether title looks like an event-style task (meeting, call,
// session, etc.) rather than a plain action item.
func isEventTask(title string) bool {
	return eventTaskPattern.MatchString(title)
}

// PastEventService proposes confirm-first "close it?" candidates for event-style
// tasks whose scheduled date has passed. Why: events get rescheduled, so this never
// auto-closes -- it only surfaces the UI chip the user already knows how to act on.
type PastEventService struct{}

func NewPastEventService() *PastEventService {
	return &PastEventService{}
}

// ProposePastEventCandidates scans email's active TASK rows for event-style titles
// whose deadline_date has fully passed (at least one full day, so a same-day event
// running late is not flagged mid-day) and records a pending completion candidate.
// Returns the number of candidates added.
func (s *PastEventService) ProposePastEventCandidates(ctx context.Context, email string, now time.Time) (int, error) {
	// Why: UTC keeps the cutoff stable regardless of the deployment host's local zone.
	today := now.UTC().Truncate(24 * time.Hour)
	cutoff := today.AddDate(0, 0, -1)
	lookbackFloor := cutoff.AddDate(0, 0, -pastEventLookbackDays)

	rows, err := store.ListPastEventCandidates(ctx, email, cutoff, lookbackFloor, pastEventScanLimit)
	if err != nil {
		return 0, fmt.Errorf("list past event candidates: %w", err)
	}

	proposed := 0
	for _, r := range rows {
		if !isEventTask(r.Task) {
			continue
		}
		sourceKey := "past-event:" + r.DeadlineDate.Format("2006-01-02")
		if store.WasCandidateDismissed(r.Metadata, sourceKey) {
			continue
		}
		cand := store.CompletionCandidate{
			SourceLink: sourceKey,
			SourceText: truncateRunes(r.Task, candidateEvidenceMax),
			Evidence:   "scheduled date passed",
			DetectedAt: now.UTC().Format(time.RFC3339),
			Status:     "pending",
		}
		if err := store.AddCompletionCandidate(ctx, store.GetDB(), email, r.ID, cand); err != nil {
			logger.Warnf("[PAST-EVENT] propose failed msg=%d: %v", r.ID, err)
			continue
		}
		proposed++
	}
	if proposed > 0 {
		logger.Infof("[PAST-EVENT] %s: proposed %d candidate(s)", email, proposed)
	}
	return proposed, nil
}
