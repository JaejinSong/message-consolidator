package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
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

// Why: truncates on rune boundaries so multi-byte titles (e.g. Korean) never split mid-character.
func (s *TasksService) truncateTitle(t string, max int) string {
	r := []rune(t)
	if len(r) <= max {
		return t
	}
	return string(r[:max-3]) + "..."
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
	if match, fuzzy := s.findMatch(room, item, active); match != nil {
		item.ID = &match.ID
		// Why: a fuzzy (title-similarity-only) match has no ID/thread anchor -- keep
		// IDVerified false so G5 resolve-trust still applies, and flag FuzzyMatched so
		// handleUpdate appends instead of renaming (wrong-task rename incident).
		item.IDVerified = !fuzzy
		item.FuzzyMatched = fuzzy
		// Upgrade 'new' to 'update' if we found an existing task.
		// Keep 'resolve', 'cancel', 'update' as AI intended.
		if item.State == "new" {
			item.State = "update"
		}
		// Why: counterparty chatter outside the task's reply chain must not hard-close
		// a task (false auto-resolve regression); it lands as a confirm-first candidate.
		if item.State == "resolve" {
			item.State = resolveStateFor(room, item, match)
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

// resolveStateFor keeps "resolve" for trusted proposals and demotes the rest to "resolve_candidate".
func resolveStateFor(room string, item store.TodoItem, match *store.ConsolidatedMessage) string {
	rule := trustedResolveRule(item, match)
	switch rule {
	case "":
		return "resolve_candidate"
	case "query_answered", "shared_member_report":
		logger.Infof("[RESOLVE-TRUST] rule=%s task=%d room=%s", rule, match.ID, room)
	}
	return "resolve"
}

// strongTopicalOverlap is the token overlap that, absent a shared thread, anchors a proposal strongly enough for open-room resolve trust.
const strongTopicalOverlap = minTopicalOverlap + 1

// findMatch returns the matched task and whether the match was fuzzy (title-similarity-only,
// no ID/thread anchor). Why: a fuzzy match must never rename the target task (see FuzzyMatched).
func (s *TasksService) findMatch(room string, item store.TodoItem, active []store.ConsolidatedMessage) (*store.ConsolidatedMessage, bool) {
	// ID-first: AI explicitly identified the target task from existing context.
	// Why: a rejected AI-supplied ID must not fall through to the fuzzy loop -- the model
	// already anchored this proposal to a specific (wrong) task, and fuzzy title similarity
	// alone (e.g. "Arrange dinner..." vs "Arrange lunch" = 0.966 Jaro-Winkler) previously
	// re-matched it anyway, renaming an unrelated task (Slack production incident).
	if item.ID != nil && *item.ID != 0 {
		if m := verifiedIDMatch(room, item, active); m != nil {
			return m, false
		}
		return nil, false
	}

	for i := range active {
		if isFuzzyMatch(room, item, &active[i]) {
			return &active[i], true
		}
	}
	return nil, false
}

// isFuzzyMatch reports whether an un-anchored proposal matches an open task by title.
func isFuzzyMatch(room string, item store.TodoItem, m *store.ConsolidatedMessage) bool {
	if m.Room != room || m.Category != item.Category || isArchivedCandidate(m) {
		return false
	}
	// Why: prevent cross-thread merges in proposal resolution (mirrors isSemanticDup guard).
	if item.ThreadID != "" && m.ThreadID != "" && item.ThreadID != m.ThreadID {
		return false
	}
	// Why: similarity alone scores unrelated-but-prefix-sharing titles too high
	// (see findMatch) -- require topical token overlap too.
	return store.CalculateSimilarity(item.Task, m.Task) >= 0.85 && titleTokenOverlap(item.Task, m.Task) >= minTopicalOverlap
}

// isArchivedCandidate reports whether a task has been merged away. Why: merging only
// flips category to 'merged' (store/message_merge.go) and never sets done or is_deleted,
// so GetActiveTasksForContext still returns those rows -- an AI-supplied ID can bind a
// proposal to a task the UI permanently hides, discarding the new content silently.
func isArchivedCandidate(m *store.ConsolidatedMessage) bool {
	return m.Category == string(types.CategoryMerged)
}

// isTrustedResolve — only the user's own statement, an in-thread reply, the assignee's
// own completion report, or an ID-anchored QUERY answer / group-member report may
// hard-close a task; anything else becomes a confirm-first candidate (resolve_candidate).
func isTrustedResolve(item store.TodoItem, match *store.ConsolidatedMessage) bool {
	return trustedResolveRule(item, match) != ""
}

// trustedResolveRule names the trust rule that lets a resolve proposal hard-close a task, or "" when none applies.
func trustedResolveRule(item store.TodoItem, match *store.ConsolidatedMessage) string {
	switch {
	case item.IsFromMe:
		return "from_me"
	case item.ThreadID != "" && match.ThreadID != "" && item.ThreadID == match.ThreadID:
		return "same_thread"
	case senderIsAssignee(item.SenderName, match.Assignee):
		return "assignee_report"
	case isQueryAnswered(item, match):
		return "query_answered"
	case isSharedMemberReport(item, match):
		return "shared_member_report"
	}
	return ""
}

// hasStrongAnchor reports whether an ID-verified proposal is tied to the matched task by thread or
// by more than the minimal topical overlap. Why: IDVerified alone is also true for a blank-title
// bare resolve or a bare-minimum overlap, too weak to let any room member close a task.
func hasStrongAnchor(item store.TodoItem, match *store.ConsolidatedMessage) bool {
	if !item.IDVerified || item.FuzzyMatched || strings.TrimSpace(item.Task) == "" {
		return false
	}
	if item.ThreadID != "" && item.ThreadID == match.ThreadID {
		return true
	}
	return titleTokenOverlap(item.Task, match.Task) >= strongTopicalOverlap
}

// isQueryAnswered: a QUERY is done once someone other than the asker answers it.
func isQueryAnswered(item store.TodoItem, match *store.ConsolidatedMessage) bool {
	if match.Category != string(types.CategoryQuery) || !hasStrongAnchor(item, match) {
		return false
	}
	sender := NormalizeSenderIdentity(item.SenderName)
	return sender != "" && sender != NormalizeSenderIdentity(match.Requester)
}

// isSharedMemberReport: a group-assigned task reported done by any member other than its requester.
func isSharedMemberReport(item store.TodoItem, match *store.ConsolidatedMessage) bool {
	if NormalizeSenderIdentity(match.Assignee) != AssigneeShared || !hasStrongAnchor(item, match) {
		return false
	}
	sender := NormalizeSenderIdentity(item.SenderName)
	requester := NormalizeSenderIdentity(match.Requester)
	// Why: the literal "shared" is a placeholder, not a real group member.
	return sender != "" && sender != AssigneeShared && requester != "" && sender != requester
}

// senderIsAssignee reports whether the raw message sender is the task's assignee.
// Why: the assignee's own report of completion is first-party evidence, same trust as
// own reply -- WhatsApp groups rarely quote-reply, so this is the only signal available.
func senderIsAssignee(sender, assignee string) bool {
	assignee = NormalizeSenderIdentity(assignee)
	if assignee == "" || assignee == AssigneeShared {
		return false
	}
	sender = NormalizeSenderIdentity(sender)
	if sender == "" {
		return false
	}
	return sender == assignee
}

// NormalizeSenderIdentity collapses display-name noise (case, the "(Ambiguous)" report-time
// suffix, and whitespace) so a sender name and an assignee name can be compared exactly.
func NormalizeSenderIdentity(raw string) string {
	name := strings.ToLower(stripAmbiguityMarker(raw))
	return strings.Join(strings.Fields(name), " ")
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
	if m.Room != room || isArchivedCandidate(m) {
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
	return countTokenHits(ftsCandidateTokens(a, maxCrossThreadFTSTokens), b)
}
