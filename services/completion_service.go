package services

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var (
	subjectBracketRe = regexp.MustCompile(`(?i)^\s*(\[[^\]]*\]\s*)+`)
	subjectReplyRe   = regexp.MustCompile(`(?i)^(re|fwd|fw):\s*`)
)

// ackOnlyTokens is the set of short acknowledgment tokens that do NOT constitute
// substantive task resolution. When a fromMe reply is composed solely of these
// tokens (after stripping whitespace, greetings, and signatures), it should be
// reclassified without AI evaluation — AI RESOLVE on "ok/thanks" would incorrectly
// close the sender's own task.
var ackOnlyTokens = map[string]bool{
	"네": true, "넵": true, "알겠습니다": true, "확인했습니다": true,
	"감사합니다": true, "감사해요": true, "고맙습니다": true, "오케이": true,
	"ok": true, "okay": true, "thanks": true, "thx": true, "thank you": true,
	"noted": true, "got it": true, "sure": true, "fine": true,
	"understood": true, "will do": true, "👍": true, "✅": true, "✔️": true,
}

// signaturePrefixes marks the start of email signature blocks to strip before ack detection.
var signaturePrefixes = []string{"From:", "Sent from", "보내는 사람:"}

// greetingPrefixes strips common openers that are not substantive content.
var greetingPrefixes = []string{"안녕하세요", "Hi", "Hello", "Hey"}

// maxAckLength is the character budget after stripping. Anything longer is
// treated as a substantive reply that deserves AI evaluation.
const maxAckLength = 50

// isAckOnlyReply returns true when the reply text, after stripping whitespace,
// common greetings, and email signature blocks, consists solely of short
// acknowledgment tokens. Maximum stripped length is maxAckLength characters.
func isAckOnlyReply(text string) bool {
	stripped := stripSignature(text)
	stripped = stripGreeting(stripped)
	stripped = strings.TrimSpace(stripped)
	if len([]rune(stripped)) > maxAckLength {
		return false
	}
	return matchesAckTokens(stripped)
}

func stripSignature(text string) string {
	for _, prefix := range signaturePrefixes {
		if idx := strings.Index(text, prefix); idx != -1 {
			text = text[:idx]
		}
	}
	return text
}

func stripGreeting(text string) string {
	trimmed := strings.TrimSpace(text)
	for _, g := range greetingPrefixes {
		if strings.HasPrefix(trimmed, g) {
			rest := strings.TrimPrefix(trimmed, g)
			rest = strings.TrimLeft(rest, " \t\n\r,.")
			trimmed = rest
		}
	}
	return trimmed
}

// matchesAckTokens checks if the text (after stripping) is composed solely of
// known ack tokens plus punctuation noise (!, ., ,, ~, ^^, 🙏).
func matchesAckTokens(text string) bool {
	// Remove punctuation noise to isolate token words.
	noise := strings.NewReplacer("!", "", ".", "", ",", "", "~", "", "^", "", "🙏", "", " ", "")
	cleaned := strings.ToLower(noise.Replace(text))
	if cleaned == "" {
		return false
	}
	// Check against direct map lookup for the whole cleaned string.
	if ackOnlyTokens[cleaned] {
		return true
	}
	// Multi-token: split on whitespace and check each word.
	words := strings.Fields(strings.ToLower(text))
	for _, w := range words {
		wClean := noise.Replace(w)
		if wClean == "" {
			continue
		}
		if !ackOnlyTokens[wClean] {
			return false
		}
	}
	return len(words) > 0
}

// completionSignalTokens are substrings that suggest a message reports the completion
// or resolution of some task. Used as a cheap gate before the FTS + LLM cross-channel
// match, so unrelated chatter never pays for a candidate search.
var completionSignalTokens = []string{
	"완료", "처리했", "처리 했", "끝냈", "끝났", "마쳤", "마무리", "해결", "반영", "배포",
	"제출", "전달드", "전달했", "보냈", "보내드", "송부", "회신", "업로드", "공유드", "완납",
	"done", "finished", "completed", "resolved", "fixed", "deployed", "submitted",
	"sent", "shipped", "uploaded", "closed", "handled", "delivered", "wrapped up",
}

// hasCompletionSignal reports whether text plausibly announces a completion. Case-insensitive
// substring match — deliberately high-recall; the LLM transition check is the precision gate.
func hasCompletionSignal(text string) bool {
	lower := strings.ToLower(text)
	for _, tok := range completionSignalTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

func extractSubjectFromText(originalText string) string {
	for _, line := range strings.Split(originalText, "\n") {
		if strings.HasPrefix(line, "S: ") {
			s := strings.TrimPrefix(line, "S: ")
			s = subjectBracketRe.ReplaceAllString(s, "")
			s = subjectReplyRe.ReplaceAllString(s, "")
			return strings.TrimSpace(s)
		}
	}
	return ""
}

type AICompleter interface {
	AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error)
	EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string, subtasks []store.Subtask) (ai.TaskTransition, error)
	Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error)
	HasAnyTaskInThread(ctx context.Context, q store.Querier, email, threadID string) (bool, error)
	GetRecentIncompleteGmail(ctx context.Context, q store.Querier, email string) ([]store.ConsolidatedMessage, error)
	GetLatestThreadAssignee(ctx context.Context, q store.Querier, email, threadID string) (string, error)
	UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error
	HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error)
	UpdateSubtasks(ctx context.Context, q store.Querier, email string, id store.MessageID, subtasks []store.Subtask) error
	AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error
	SearchOpenTasksFTS(ctx context.Context, email string, tokens []string, limit int) ([]store.ConsolidatedMessage, error)
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) HasAnyTaskInThread(ctx context.Context, q store.Querier, email, threadID string) (bool, error) {
	return store.HasAnyTaskInThread(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) GetLatestThreadAssignee(ctx context.Context, q store.Querier, email, threadID string) (string, error) {
	return store.GetLatestThreadAssignee(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error {
	return store.UpdateMessageCategory(ctx, q, email, id, category)
}

func (d *DefaultTaskStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	return HandleTaskState(ctx, q, email, item, msg)
}

func (d *DefaultTaskStore) GetRecentIncompleteGmail(ctx context.Context, q store.Querier, email string) ([]store.ConsolidatedMessage, error) {
	return store.GetRecentIncompleteGmail(ctx, q, email)
}

func (d *DefaultTaskStore) UpdateSubtasks(ctx context.Context, q store.Querier, email string, id store.MessageID, subtasks []store.Subtask) error {
	return store.UpdateSubtasks(ctx, q, email, id, subtasks)
}

func (d *DefaultTaskStore) AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error {
	return store.AddCompletionCandidate(ctx, q, email, id, cand)
}

func (d *DefaultTaskStore) SearchOpenTasksFTS(ctx context.Context, email string, tokens []string, limit int) ([]store.ConsolidatedMessage, error) {
	return store.SearchOpenTasksFTS(ctx, email, tokens, limit)
}

type CompletionService struct {
	gemini   AICompleter
	store    TaskStore
	tasksSvc *TasksService
	db       *sql.DB
}

func NewCompletionService(gemini AICompleter, taskStore TaskStore, tasksSvc *TasksService, db *sql.DB) *CompletionService {
	return &CompletionService{gemini: gemini, store: taskStore, tasksSvc: tasksSvc, db: db}
}

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

// ftsCrossThreadCandidates ranks open tasks by BM25 relevance to the incoming message's
// keyword tokens, excluding the message's own thread. Precision relies on the completion-
// signal gate upstream and the LLM transition check downstream, not on a score threshold.
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
	for _, c := range cands {
		if msg.ThreadID != "" && c.ThreadID == msg.ThreadID {
			continue
		}
		out = append(out, c)
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

// handleCrossThreadCandidates evaluates whether msg affects an open task in another
// thread/channel and applies the confirm-first policy: RESOLVE (a close) is recorded as
// a pending candidate for one-tap user confirmation and never auto-closed, while UPDATE
// (a scope refinement, not a close) auto-applies as before. Returns true when handled.
func (s *CompletionService) handleCrossThreadCandidates(ctx context.Context, msg store.ConsolidatedMessage, candidates []store.ConsolidatedMessage) bool {
	top := candidates[0]
	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, top.Task, msg.OriginalText, top.Subtasks)
	if err != nil {
		compStats.llmError.Add(1)
		return false
	}
	switch res.Status {
	case "RESOLVE":
		compStats.llmResolve.Add(1)
		return s.recordCompletionCandidate(ctx, msg, top)
	case "UPDATE":
		compStats.llmUpdate.Add(1)
		handled := false
		for _, task := range candidates {
			if s.handleCompletionResult(ctx, res, msg, task) {
				handled = true
			}
		}
		return handled
	}
	compStats.llmNone.Add(1)
	return false
}

// recordCompletionCandidate writes a confirm-first completion candidate onto the task's
// metadata instead of closing it. Returns true on success so the caller suppresses
// re-extraction (the message is about this existing task, not a new one).
func (s *CompletionService) recordCompletionCandidate(ctx context.Context, msg, task store.ConsolidatedMessage) bool {
	// Why: the user already dismissed this exact source once — re-recording it would
	// resurrect a suggestion they explicitly rejected.
	if store.WasCandidateDismissed(string(task.Metadata), msg.Link) {
		compStats.dismissSuppressed.Add(1)
		logger.Debugf("[COMPLETION] candidate suppressed (previously dismissed) for task %d from %s", task.ID, msg.Link)
		return false
	}
	cand := store.CompletionCandidate{
		SourceLink: msg.Link,
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

// ProcessCrossChannelSignal evaluates a plain chat message against open tasks in
// other threads/channels. Confirm-first only: records pending candidates, never
// auto-closes, never falls back to extraction.
func (s *CompletionService) ProcessCrossChannelSignal(ctx context.Context, msg store.ConsolidatedMessage) (bool, error) {
	if !hasCompletionSignal(msg.OriginalText) {
		compStats.crossSignalMiss.Add(1)
		return false, nil
	}
	candidates := s.findCrossThreadCandidates(ctx, msg)
	if len(candidates) == 0 {
		return false, nil
	}
	return s.handleCrossThreadCandidates(ctx, msg, candidates), nil
}

// HasCompletionSignal exposes hasCompletionSignal to scanner adapters so they can
// gate cross-channel dispatch without duplicating the keyword list.
func HasCompletionSignal(text string) bool { return hasCompletionSignal(text) }

// ProcessPotentialCompletion checks if a message (reply) completes/updates tasks in the same thread.
// Why: [Early Return] Returns true if the message was handled as a task completion/update, signaling the scanner to skip extraction.
func (s *CompletionService) ProcessPotentialCompletion(ctx context.Context, msg store.ConsolidatedMessage) (bool, error) {
	if msg.ThreadID == "" && msg.RepliedToID == "" {
		return false, nil
	}
	targetID := msg.ThreadID
	if targetID == "" {
		targetID = msg.RepliedToID
	}

	tasks, _ := s.store.GetIncompleteByThreadID(ctx, s.db, msg.UserEmail, targetID)
	if len(tasks) == 0 {
		// Why: cross-thread candidate check runs before the fromMe self-summary skip
		// below — previously that skip returned early and a fromMe self-summary could
		// never surface a confirm-first match against an open task in another thread.
		if candidates := s.findCrossThreadCandidates(ctx, msg); len(candidates) > 0 {
			if s.handleCrossThreadCandidates(ctx, msg, candidates) {
				return true, nil
			}
		}
		if strings.EqualFold(msg.RequesterCanonical, msg.UserEmail) {
			// Why: skip only when this thread has NEVER had a task (truly a self-summary
			// like a weekly report). If any prior task exists (incl. done), the user's
			// follow-up is a reopen signal — let normal extraction run.
			hasAny, _ := s.store.HasAnyTaskInThread(ctx, s.db, msg.UserEmail, targetID)
			if !hasAny {
				return true, nil
			}
		}
		// Why: Fallback consumes its own AI Analyze + persists tasks. Returning true
		// signals the caller to MarkAsProcessed so the next scan cycle skips this msg
		// instead of paying for LiteFilter + Analyze + batch Analyze again.
		return s.fallbackToNewExtraction(ctx, msg), nil
	}

	compStats.entryThreadPath.Add(1)
	fromMe := strings.EqualFold(msg.RequesterCanonical, msg.UserEmail)
	if fromMe {
		// Why: Ack-only fromMe replies ("ok", "감사합니다", etc.) must not reach AI
		// because a RESOLVE response would incorrectly close the sender's own task.
		// The explicit ✅ dashboard button is the correct close path for these.
		if isAckOnlyReply(msg.OriginalText) {
			for _, task := range tasks {
				_ = s.store.UpdateMessageCategory(ctx, s.db, msg.UserEmail, task.ID, CategoryRequested)
			}
			return true, nil
		}
		// Why: Substantive fromMe replies (redirect/delegation/resolution) need AI
		// judgment. With multiple tasks, evaluate each independently so a partial
		// resolution does not blindly close unrelated sibling tasks.
		if len(tasks) > 1 {
			return s.evaluatePerTask(ctx, msg, tasks)
		}
		// Single-task: fall through to the shared single-call path below.
	}

	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, tasks[0].Task, msg.OriginalText, tasks[0].Subtasks)
	if err != nil {
		return false, fmt.Errorf("transition analysis failed: %w", err)
	}

	// Why: Apply the same transition to all incomplete tasks in the thread —
	// a single reply affects every open item from that conversation.
	handled := false
	for _, task := range tasks {
		if s.handleCompletionResult(ctx, res, msg, task) {
			handled = true
		}
	}
	return handled, nil
}

// evaluatePerTask calls EvaluateTaskTransition individually for each task so that
// substantive fromMe multi-task replies can resolve some tasks while leaving others open.
func (s *CompletionService) evaluatePerTask(ctx context.Context, msg store.ConsolidatedMessage, tasks []store.ConsolidatedMessage) (bool, error) {
	handled := false
	for _, task := range tasks {
		res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, task.Task, msg.OriginalText, task.Subtasks)
		if err != nil {
			return false, fmt.Errorf("transition analysis failed: %w", err)
		}
		if s.handleCompletionResult(ctx, res, msg, task) {
			handled = true
		}
	}
	return handled, nil
}

func (s *CompletionService) handleCompletionResult(ctx context.Context, res ai.TaskTransition, msg, parent store.ConsolidatedMessage) bool {
	parentID := parent.ID
	switch res.Status {
	case "RESOLVE":
		// Why: cascade subtasks to done before marking parent resolved so reverse-propagation
		// (all subtasks done → parent auto-close) sees a consistent terminal state.
		if len(parent.Subtasks) > 0 {
			allDone := make([]store.Subtask, len(parent.Subtasks))
			copy(allDone, parent.Subtasks)
			for i := range allDone {
				allDone[i].Done = true
			}
			_ = s.store.UpdateSubtasks(ctx, s.db, msg.UserEmail, parentID, allDone)
		}
		item := store.TodoItem{State: "resolve", ID: &parentID}
		_, _ = s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
		return true
	case "UPDATE":
		if res.UpdatedText == "" {
			return false
		}
		if len(res.SubtaskUpdates) > 0 && len(parent.Subtasks) > 0 {
			updated := make([]store.Subtask, len(parent.Subtasks))
			copy(updated, parent.Subtasks)
			for _, su := range res.SubtaskUpdates {
				if su.Index >= 0 && su.Index < len(updated) {
					updated[su.Index].Done = su.Done
				}
			}
			_ = s.store.UpdateSubtasks(ctx, s.db, msg.UserEmail, parentID, updated)
		}
		item := store.TodoItem{State: "update", ID: &parentID, Task: res.UpdatedText}
		_, _ = s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
		return true
	case "NEW":
		return s.fallbackToNewExtraction(ctx, msg)
	}
	return false
}

// fallbackToNewExtraction runs an isolated AI extraction for messages whose thread
// has no incomplete parent task. Returns true once AI Analyze succeeds so callers
// can MarkAsProcessed and avoid paying tokens again next scan cycle (the prior
// void return left the message in filteredMsgs, causing a second batch Analyze
// in processBatch within the same cycle and re-extraction every cycle thereafter).
func (s *CompletionService) fallbackToNewExtraction(ctx context.Context, msg store.ConsolidatedMessage) bool {
	// Why: thread had a real (done) parent task — propagate its assignee so the
	// new row inherits routing context. AI per-item Assignee still wins via
	// createTaskFromItem override; this only fills the envelope default.
	if msg.Assignee == "" && msg.ThreadID != "" {
		if a, err := s.store.GetLatestThreadAssignee(ctx, s.db, msg.UserEmail, msg.ThreadID); err == nil && a != "" {
			msg.Assignee = a
		}
	}
	enriched := types.EnrichedMessage{
		RawContent: msg.OriginalText, SourceChannel: msg.Source,
		SenderName: msg.Requester, VirtualThreadID: msg.ThreadID, Timestamp: msg.CreatedAt,
	}
	room := msg.Room
	if room == "" {
		room = "General"
	}
	items, err := s.gemini.Analyze(ctx, msg.UserEmail, enriched, "Korean", msg.Source, room)
	if err != nil || len(items) == 0 {
		return false
	}

	// Why: items are independent SaveMessage calls — wrapping them in a single
	// outer tx only widened the libsql writer-lock window and silently swallowed
	// per-item INSERT failures via tx.Commit() on a nil-return fn. WithDBRetry
	// absorbs transient `database is locked` errors (5 attempts, 100ms→1.6s
	// backoff). AI cost is sunk regardless of save outcome — the bool return is
	// what stops the token bleed, not save success.
	for _, item := range items {
		err := store.WithDBRetry("CompletionFallback.HandleTaskState", func() error {
			_, e := s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
			return e
		})
		if err != nil {
			logger.Warnf("[COMPLETION] fallback: HandleTaskState dropped item after retries: %v", err)
		}
	}
	compStats.fallbackExtraction.Add(1)
	return true
}
