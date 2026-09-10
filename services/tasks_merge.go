package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"unicode"
)

// MergeTasks consolidates multiple tasks into one using AI summarization for the title.
// Why: [Contextual Merge] Generates a representative English title from all merged messages.
func (s *TasksService) MergeTasks(ctx context.Context, email string, targetIDs []store.MessageID, destID store.MessageID) error {
	if destID <= 0 || len(targetIDs) == 0 {
		return fmt.Errorf("invalid merge parameters: targetCount=%d, destID=%d", len(targetIDs), destID)
	}
	allIDs := append([]store.MessageID{}, targetIDs...)
	allIDs = append(allIDs, destID)
	msgs, err := store.GetMessagesByIDs(ctx, store.GetDB(), email, allIDs)
	if err != nil {
		return err
	}

	var dest *store.ConsolidatedMessage
	var sources []store.ConsolidatedMessage
	for i := range msgs {
		if msgs[i].ID == destID {
			dest = &msgs[i]
		} else {
			sources = append(sources, msgs[i])
		}
	}
	if dest == nil {
		return fmt.Errorf("destination task not found")
	}

	// Why: [Reliability] AI summary is progressive; failures fallback to existing title.
	newTitle := s.generateSummaryTitle(ctx, email, dest, sources)
	return store.MergeTasksWithTitle(ctx, email, targetIDs, destID, newTitle)
}

func (s *TasksService) generateSummaryTitle(ctx context.Context, email string, dest *store.ConsolidatedMessage, sources []store.ConsolidatedMessage) string {
	inputs := []struct{ Title, Text string }{{dest.Task, dest.OriginalText}}
	for _, src := range sources {
		inputs = append(inputs, struct{ Title, Text string }{src.Task, src.OriginalText})
	}

	data, _ := json.Marshal(inputs)
	title, err := s.geminiClient.GenerateMergedTaskTitle(ctx, email, string(data))
	// Why: TrimSpace guard rejects whitespace-only AI output that would otherwise
	// pass `title != ""` and silently wipe the destination task title.
	if err == nil && strings.TrimSpace(title) != "" {
		return title
	}

	// Why: [Reliability] AI response blocks/timeouts fallback to simple title concatenation to prevent info loss.
	logger.Warnf("[TASKS] AI Merge Summary Failed (Error: %v). Falling back to concatenation.", err)
	titles := make([]string, 0, len(sources)+1)
	if t := strings.TrimSpace(dest.Task); t != "" {
		titles = append(titles, t)
	}
	for _, src := range sources {
		if t := strings.TrimSpace(src.Task); t != "" {
			titles = append(titles, t)
		}
	}
	// Why: All inputs blank → preserve dest.Task verbatim (caller's last-line guard
	// will reject if even that is empty). Never collapse to "" here.
	if len(titles) == 0 {
		return dest.Task
	}
	return s.truncateTitle(strings.Join(titles, " | "), 250)
}

func (s *TasksService) truncateTitle(t string, max int) string {
	if len(t) <= max {
		return t
	}
	return t[:max-3] + "..."
}

// ResolveProposals resolves extraction results against current active tasks.
// AI proposes (rawItems), Backend decides (returns refined items with correct IDs/States).
func (s *TasksService) ResolveProposals(ctx context.Context, email, room string, rawItems []store.TodoItem, active []store.ConsolidatedMessage) []store.TodoItem {
	var results []store.TodoItem
	for _, item := range rawItems {
		results = append(results, s.resolveProposalItem(room, item, active))
	}
	return results
}

// Why: Pulls the per-item match/state decision out of ResolveProposals so the loop body stays flat (≤3 nested levels).
func (s *TasksService) resolveProposalItem(room string, item store.TodoItem, active []store.ConsolidatedMessage) store.TodoItem {
	if match := s.findMatch(room, item, active); match != nil {
		item.ID = &match.ID
		// Upgrade 'new' to 'update' if we found an existing task.
		// Keep 'resolve', 'cancel', 'update' as AI intended.
		if item.State == "new" {
			item.State = "update"
		}
		// Why: counterparty chatter outside the task's reply chain must not hard-close
		// a task (false auto-resolve regression); it lands as a confirm-first candidate.
		if item.State == "resolve" && !isTrustedResolve(item, match) {
			item.State = "resolve_candidate"
		}
		return item
	}
	// Logic: If no match found, states requiring an ID must be downgraded.
	if item.State != "update" && item.State != "resolve" && item.State != "cancel" {
		return item
	}
	if item.Task != "" && item.State == "update" {
		item.State = "new" // Only 'update' can safely downgrade to 'new'
	} else {
		item.State = "none" // resolve/cancel with no match is dropped
	}
	return item
}

func (s *TasksService) findMatch(room string, item store.TodoItem, active []store.ConsolidatedMessage) *store.ConsolidatedMessage {
	// ID-first: AI explicitly identified the target task from existing context.
	// A rejected ID falls through to the fuzzy path instead of being trusted blindly.
	if item.ID != nil && *item.ID != 0 {
		if m := verifiedIDMatch(room, item, active); m != nil {
			return m
		}
	}

	for i := range active {
		m := &active[i]
		if m.Room != room {
			continue
		}
		// Why: prevent cross-thread merges in proposal resolution (mirrors isSemanticDup guard).
		if item.ThreadID != "" && m.ThreadID != "" && item.ThreadID != m.ThreadID {
			continue
		}

		if m.Category == item.Category && store.CalculateSimilarity(item.Task, m.Task) >= 0.85 {
			return m
		}
		// Why: the model labels the same request TASK on one pass and QUERY on the next,
		// so the category equality above lets a reworded duplicate through as a new task
		// (the TGIA CIO Forum decision landed four times). Jaro-Winkler cannot arbitrate
		// that -- see isTrustedIDMatch -- but content-token overlap can.
		if duplicateTopicOverlap(item.Task, m.Task) >= minDuplicateOverlap {
			return m
		}
	}
	return nil
}

// minDuplicateOverlap is how many distinct content tokens two titles must share before
// findMatch treats them as the same task despite disagreeing categories. Why: measured
// on production titles, real duplicates share 5-8 tokens while unrelated tasks in the
// same room share 0-2 (2026-09-10), so 4 sits in the gap with margin on both sides.
const minDuplicateOverlap = 4

// duplicateStopwords are function words long enough to clear the 3-rune token floor
// while carrying no topic. Why: the shared FTS tokenizer keeps them, which would inflate
// the overlap count for any two generically worded English titles.
var duplicateStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "via": true, "with": true, "from": true,
	"into": true, "onto": true, "out": true, "all": true, "any": true, "are": true,
	"was": true, "has": true, "have": true, "been": true, "will": true, "can": true,
	"not": true, "its": true, "our": true, "their": true, "this": true, "that": true,
	"these": true, "those": true, "than": true, "then": true, "when": true, "while": true,
	"about": true, "after": true, "before": true, "during": true, "over": true,
	"under": true, "per": true, "upon": true, "his": true, "her": true, "you": true,
	"your": true,
}

// duplicateTopicOverlap counts distinct content tokens present in both titles. Uses an
// exact token intersection rather than titleTokenOverlap's substring containment, so
// "art" cannot ground itself inside "start".
func duplicateTopicOverlap(a, b string) int {
	bTokens := contentTokenSet(b)
	if len(bTokens) == 0 {
		return 0
	}
	overlap := 0
	for token := range contentTokenSet(a) {
		if bTokens[token] {
			overlap++
		}
	}
	return overlap
}

func contentTokenSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
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

// isTrustedResolve — only the user's own statement or an in-thread reply may hard-close
// a task; anything else becomes a confirm-first candidate (resolve_candidate).
func isTrustedResolve(item store.TodoItem, match *store.ConsolidatedMessage) bool {
	if item.IsFromMe {
		return true
	}
	return item.ThreadID != "" && match.ThreadID != "" && item.ThreadID == match.ThreadID
}

// verifiedIDMatch trusts an AI-supplied task ID only when the proposal is anchored to the
// same reply thread or lexically tied to the matched title. Why: the model occasionally
// binds an unrelated message to whichever open task sits in context (Indofood-PO merge);
// an unverified ID appends that message's history onto — or resolves — the wrong task.
func verifiedIDMatch(room string, item store.TodoItem, active []store.ConsolidatedMessage) *store.ConsolidatedMessage {
	for i := range active {
		m := &active[i]
		if m.ID != *item.ID {
			continue
		}
		if isTrustedIDMatch(room, item, m) {
			return m
		}
		logger.LogDecision(logger.DecisionLog{
			Room: room, State: item.State, TaskID: (*int64)(item.ID), Task: item.Task,
			Reasoning: fmt.Sprintf("ai id rejected: no thread/topic tie to %q", m.Task),
		})
		return nil
	}
	return nil
}

func isTrustedIDMatch(room string, item store.TodoItem, m *store.ConsolidatedMessage) bool {
	if m.Room != room {
		return false
	}
	if item.ThreadID != "" && m.ThreadID != "" && item.ThreadID == m.ThreadID {
		return true
	}
	// Why: nothing to verify lexically — a bare resolve/cancel carries no title.
	if strings.TrimSpace(item.Task) == "" {
		return true
	}
	// Why: token overlap only — Jaro-Winkler scores unrelated sentences 0.55+
	// (measured: Indofood-PO vs SAMCO title = 0.57), so a similarity floor
	// cannot separate a rephrased title from a different topic.
	return titleTokenOverlap(item.Task, m.Task) >= minTopicalOverlap
}

// titleTokenOverlap counts distinct ≥3-rune tokens of a shared between the two titles.
func titleTokenOverlap(a, b string) int {
	haystack := strings.ToLower(b)
	overlap := 0
	for _, t := range ftsCandidateTokens(a, maxCrossThreadFTSTokens) {
		if strings.Contains(haystack, strings.ToLower(t)) {
			overlap++
		}
	}
	return overlap
}
