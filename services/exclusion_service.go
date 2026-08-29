package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"message-consolidator/logger"
	"message-consolidator/store"
)

// Prime intervals per project convention.
const (
	// exclusionCandidateDays is the no-update age at which a TASK becomes an exclusion candidate.
	exclusionCandidateDays = 31
	// exclusionReproposeDays bounds how long a dismissal suppresses re-proposal without new activity.
	exclusionReproposeDays = 61
	// excludedDigestIntervalDays paces the "still parked" digest per item.
	excludedDigestIntervalDays = 29
	// excludedDigestCap bounds items per digest DM to keep messages readable.
	excludedDigestCap = 7
	// excludedDigestWindowKey is the reminded_at_<window> metadata suffix for the digest.
	excludedDigestWindowKey = "excluded_digest"
)

// ExclusionService proposes tracking-exclusion candidates for long-term-unprocessed
// tasks (confirm-first, chip-only) and sends the periodic digest for parked ones.
type ExclusionService struct {
	Slack SlackPoster
}

func NewExclusionService(slack SlackPoster) *ExclusionService {
	return &ExclusionService{Slack: slack}
}

// ProposeExclusionCandidates marks open TASK rows unchanged for 31+ days as exclusion
// candidates. No Slack DM at proposal time — the UI chip is the only surface, so the
// first deploy's backlog wave stays silent. Works without a Slack dependency.
func (s *ExclusionService) ProposeExclusionCandidates(ctx context.Context) error {
	if s == nil {
		return nil
	}
	rows, err := store.SelectExclusionScan(ctx, exclusionCandidateDays)
	if err != nil {
		return fmt.Errorf("exclusion scan: %w", err)
	}
	now := time.Now().UTC()
	proposed := 0
	for _, r := range rows {
		if !shouldProposeExclusion(r, now) {
			continue
		}
		cand := store.ExclusionCandidate{
			ProposedAt:  now.Format(time.RFC3339),
			DaysStalled: r.DaysStalled,
			Status:      "pending",
		}
		if err := store.ProposeExclusionCandidate(ctx, store.GetDB(), r.UserEmail, r.ID, cand); err != nil {
			logger.Warnf("[EXCLUSION] propose failed msg=%d: %v", r.ID, err)
			continue
		}
		proposed++
	}
	if proposed > 0 {
		logger.Infof("[EXCLUSION] proposed %d candidate(s)", proposed)
	}
	return nil
}

// shouldProposeExclusion applies the skip rules for one scanned row.
// Order matters: an existing candidate or pending completion evidence always wins;
// a dismissal is respected until new activity restarts the clock or 61 days elapse.
func shouldProposeExclusion(r store.ExclusionScanRow, now time.Time) bool {
	if store.HasExclusionCandidate(r.Metadata) {
		return false
	}
	if store.HasPendingCompletionCandidate(r.Metadata) {
		return false
	}
	dismissedAt, dismissed := store.ExclusionDismissedAt(r.Metadata)
	if !dismissed {
		return true
	}
	hasNewActivity := r.UpdatedAt.After(dismissedAt)
	withinRespectWindow := now.Sub(dismissedAt) < exclusionReproposeDays*24*time.Hour
	return hasNewActivity || !withinRespectWindow
}

// DispatchExcludedDigest sends one Slack DM per user summarizing tasks parked for
// 29+ days, re-firing every 29 days per item (reminded_at_excluded_digest recency).
func (s *ExclusionService) DispatchExcludedDigest(ctx context.Context) error {
	if s == nil || s.Slack == nil {
		return nil
	}
	items, err := store.SelectExcludedDigestItems(ctx)
	if err != nil {
		return fmt.Errorf("select excluded digest items: %w", err)
	}

	interval := excludedDigestIntervalDays * 24 * time.Hour
	due := make(map[string][]store.ExcludedItem)
	var userOrder []string
	for _, it := range items {
		if it.ExcludedAt.IsZero() || time.Since(it.ExcludedAt) < interval {
			continue
		}
		if store.RemindedWithin(it.Metadata, excludedDigestWindowKey, interval) {
			continue
		}
		if _, seen := due[it.UserEmail]; !seen {
			userOrder = append(userOrder, it.UserEmail)
		}
		due[it.UserEmail] = append(due[it.UserEmail], it)
	}

	for _, email := range userOrder {
		if err := s.dispatchExcludedDigestForUser(ctx, email, due[email]); err != nil {
			logger.Warnf("[EXCLUSION] digest failed user=%s: %v", email, err)
		}
	}
	return nil
}

func (s *ExclusionService) dispatchExcludedDigestForUser(ctx context.Context, email string, items []store.ExcludedItem) error {
	user, err := store.GetOrCreateUser(ctx, email, "", "")
	if err != nil || user == nil || strings.TrimSpace(user.SlackID) == "" {
		return nil
	}
	if len(items) > excludedDigestCap {
		logger.Warnf("[EXCLUSION] digest capped user=%s skipped=%d", email, len(items)-excludedDigestCap)
		items = items[:excludedDigestCap]
	}
	if err := s.Slack.SendDM(ctx, user.SlackID, formatExcludedDigest(items)); err != nil {
		return fmt.Errorf("send dm: %w", err) // don't mark — retry next tick
	}
	now := time.Now().UTC()
	for _, it := range items {
		if err := store.MarkReminded(ctx, email, it.ID, it.Metadata, excludedDigestWindowKey, now); err != nil {
			logger.Warnf("[EXCLUSION] digest MarkReminded failed msg=%d: %v", it.ID, err)
		}
	}
	return nil
}

func formatExcludedDigest(items []store.ExcludedItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, ":package: 추적제외된 업무 %d건 — 아직 보류 중입니다.\n", len(items))
	now := time.Now().UTC()
	for _, it := range items {
		days := int(now.Sub(it.ExcludedAt).Hours() / 24)
		line := fmt.Sprintf("• %s (%s/%s, 제외 D+%d일)", it.Task, it.Source, it.Room, days)
		if it.Link != "" {
			line += " " + it.Link
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
