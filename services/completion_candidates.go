package services

import (
	"context"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

const (
	// crossThreadTopK bounds how many open tasks the FTS search returns for LLM review.
	crossThreadTopK = 5
	// maxCrossThreadFTSTokens caps the OR-query width; BM25 already favors multi-token
	// hits, so extra tokens past this add recall noise, not precision.
	maxCrossThreadFTSTokens = 13
)

// findCrossThreadCandidates returns open tasks a cross-channel message might complete.
// Prefers FTS BM25 matching across all sources; falls back to gmail subject similarity.
func (s *CompletionService) findCrossThreadCandidates(ctx context.Context, msg store.ConsolidatedMessage) []store.ConsolidatedMessage {
	// Why: the FTS path targets cross-channel *completions*, so gate it on a cheap
	// completion signal. The gmail subject fallback stays ungated (pure string match)
	// so cross-thread UPDATE re-notices keep routing.
	if hasCompletionSignal(msg.OriginalText) {
		if cands := s.ftsCrossThreadCandidates(ctx, msg); len(cands) > 0 {
			return cands
		}
	}
	return s.gmailSubjectCandidates(ctx, msg)
}

// ftsCrossThreadCandidates ranks open tasks by BM25 relevance to the incoming message's
// keyword tokens, excluding the message's own thread and any hit that shares no topical
// tokens with the message. Why: BM25 is room-blind and unscored — without the topical
// floor a generic completion phrase fans into unrelated open tasks (Indofood-PO bug).
func (s *CompletionService) ftsCrossThreadCandidates(ctx context.Context, msg store.ConsolidatedMessage) []store.ConsolidatedMessage {
	tokens := ftsCandidateTokens(msg.OriginalText, maxCrossThreadFTSTokens)
	if len(tokens) == 0 {
		return nil
	}
	cands, err := s.store.SearchOpenTasksFTS(ctx, msg.UserEmail, tokens, crossThreadTopK)
	if err != nil {
		logger.Warnf("[COMPLETION] fts candidate search failed: %v", err)
		compStats.ftsEmpty.Add(1)
		return nil
	}
	if len(cands) == 0 {
		compStats.ftsEmpty.Add(1)
	}
	var out []store.ConsolidatedMessage
	skipped := 0
	for _, c := range cands {
		if msg.ThreadID != "" && c.ThreadID == msg.ThreadID {
			continue
		}
		if topicalOverlap(tokens, c) < minTopicalOverlap {
			skipped++
			continue
		}
		out = append(out, c)
	}
	if skipped > 0 && len(out) == 0 {
		compStats.topicalMiss.Add(1)
		logger.Debugf("[COMPLETION] all %d FTS hits off-topic for message in %s", skipped, msg.Room)
	}
	return out
}

func (s *CompletionService) gmailSubjectCandidates(ctx context.Context, msg store.ConsolidatedMessage) []store.ConsolidatedMessage {
	if msg.Source != "gmail" {
		return nil
	}
	incomingSubj := extractSubjectFromText(msg.OriginalText)
	if incomingSubj == "" {
		return nil
	}
	recent, err := s.store.GetRecentIncompleteGmail(ctx, s.db, msg.UserEmail)
	if err != nil || len(recent) == 0 {
		return nil
	}
	var candidates []store.ConsolidatedMessage
	for _, t := range recent {
		if t.ThreadID == msg.ThreadID {
			continue
		}
		existingSubj := extractSubjectFromText(t.OriginalText)
		if existingSubj == "" {
			continue
		}
		if store.CalculateSimilarity(incomingSubj, existingSubj) >= 0.85 {
			candidates = append(candidates, t)
		}
	}
	return candidates
}

// candidateEvidenceMax caps how much of the source message is stored as evidence.
const candidateEvidenceMax = 280

// minTopicalOverlap is the number of distinct message tokens that must appear in a
// task's title/subtasks before an LLM transition check may bind them. Why: BM25 is
// room-blind and unscored, so a generic completion phrase would otherwise fan into
// whatever open tasks share one incidental word (Indofood-PO regression).
const minTopicalOverlap = 2

// handleCrossThreadCandidates evaluates whether msg affects an open task in another
// thread/channel and applies the confirm-first policy: RESOLVE (a close) is recorded as
// a pending candidate for one-tap user confirmation and never auto-closed, while UPDATE
// (a scope refinement, not a close) auto-applies to the evaluated task only. Returns
// true when handled.
func (s *CompletionService) handleCrossThreadCandidates(ctx context.Context, msg store.ConsolidatedMessage, candidates []store.ConsolidatedMessage) bool {
	top := candidates[0]
	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, top.Task, msg.OriginalText, top.Subtasks)
	if err != nil {
		// Why: this path only bumped a counter, so an output-budget truncation dropped the
		// completion detection with nothing in the log to attribute it to. The sibling paths
		// return the error upward; this one cannot, so it has to say why it gave up.
		compStats.llmError.Add(1)
		logger.Warnf("[COMPLETION] cross-thread transition failed for msg %d (%s): %v", msg.ID, msg.Source, err)
		return false
	}
	switch res.Status {
	case "RESOLVE":
		compStats.llmResolve.Add(1)
		return s.recordCompletionCandidate(ctx, msg, top)
	case "UPDATE":
		compStats.llmUpdate.Add(1)
		// Why: HandleTaskState applies with the incoming message's room; validateTargetTask
		// then rejects any task whose own room differs, logging a security error and never
		// applying. Cross-room UPDATE always fails routing, so skip the call and let the
		// task's own room keep owning its title (same-room stays on the auto-apply path).
		if top.Room != msg.Room {
			compStats.crossRoomUpdateSkipped.Add(1)
			logger.Debugf("[COMPLETION] cross-room update skipped task=%d from room=%s", top.ID, top.Room)
			return false
		}
		// Why: the verdict was computed against top only — applying it to every FTS
		// hit appended unrelated conversations to unrelated tasks (Indofood-PO bug).
		return s.handleCompletionResult(ctx, res, msg, top)
	}
	compStats.llmNone.Add(1)
	return false
}

// topicalOverlap counts distinct message tokens found in the task title or subtask
// titles. Deliberately excludes original_text: appended conversation history makes it
// an ever-growing haystack where stopword-level tokens match by accident.
func topicalOverlap(tokens []string, task store.ConsolidatedMessage) int {
	var sb strings.Builder
	sb.WriteString(task.Task)
	for _, st := range task.Subtasks {
		sb.WriteString(" ")
		sb.WriteString(st.Task)
	}
	return countTokenHits(tokens, sb.String())
}

// recordCompletionCandidate writes a confirm-first completion candidate onto the task's
// metadata instead of closing it. Returns true on success so the caller suppresses
// re-extraction (the message is about this existing task, not a new one).
func (s *CompletionService) recordCompletionCandidate(ctx context.Context, msg, task store.ConsolidatedMessage) bool {
	// Why: chat sources have no permalink — the message's own ID keys dismissal
	// suppression; with an empty key WasCandidateDismissed can never suppress.
	sourceKey := msg.Link
	if sourceKey == "" {
		sourceKey = msg.SourceTS
	}
	// Why: the user already dismissed this exact source once — re-recording it would
	// resurrect a suggestion they explicitly rejected.
	if store.WasCandidateDismissed(string(task.Metadata), sourceKey) {
		compStats.dismissSuppressed.Add(1)
		logger.Debugf("[COMPLETION] candidate suppressed (previously dismissed) for task %d from %s", task.ID, sourceKey)
		return false
	}
	cand := store.CompletionCandidate{
		SourceLink: sourceKey,
		SourceText: truncateRunes(msg.OriginalText, candidateEvidenceMax),
		Evidence:   "cross-channel completion match",
		DetectedAt: time.Now().UTC().Format(time.RFC3339),
		Status:     "pending",
	}
	if err := s.store.AddCompletionCandidate(ctx, s.db, msg.UserEmail, task.ID, cand); err != nil {
		logger.Warnf("[COMPLETION] record candidate failed for task %d: %v", task.ID, err)
		return false
	}
	compStats.candidateRecorded.Add(1)
	logger.Infof("[COMPLETION] recorded confirm-first candidate for task %d from %s", task.ID, msg.Source)
	return true
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
