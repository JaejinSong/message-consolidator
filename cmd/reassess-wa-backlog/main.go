// Command reassess-wa-backlog re-runs the WhatsApp thread-reply evaluator (see
// services.CompletionService.EvaluateThreadReply) against a user's already-open
// WhatsApp tasks, reading later messages in the same chat straight from wa_messages
// instead of waiting for the next sweep tick. Dry run (default) only prints what it
// would do; -apply writes confirm-first candidates only -- it never hard-closes a
// task, even when the verdict is a RESOLVE from the task's own assignee.
//
// Usage: go run ./cmd/reassess-wa-backlog -email user@example.com [-apply] [-limit 97]
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
)

// replyCandidateCap bounds the LLM calls per task (prime).
const replyCandidateCap = 13

func main() {
	email := flag.String("email", "", "user email whose open WhatsApp tasks should be reassessed")
	apply := flag.Bool("apply", false, "write confirm-first candidates (default: dry run, no writes)")
	limit := flag.Int("limit", 97, "max number of open WhatsApp tasks to reassess")
	flag.Parse()

	if *email == "" {
		log.Fatal("usage: reassess-wa-backlog -email <user@example.com> [-apply] [-limit N]")
	}

	ctx := context.Background()
	cfg := config.LoadConfig()

	// Why: without the key, any encrypted token column is read back as ciphertext.
	store.InitTokenEncryption()
	if err := store.InitDB(ctx, cfg); err != nil {
		log.Fatalf("DB init failed: %v", err)
	}

	pc := providerConfig(cfg)
	if !pc.Enabled() {
		log.Fatal("no AI provider configured (GEMINI_API_KEY / DEEPSEEK_API_KEY)")
	}
	aiClient, err := ai.NewAIClient(ctx, pc)
	if err != nil {
		log.Fatalf("AI client init failed: %v", err)
	}

	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: *apply}
	completionSvc := services.NewCompletionService(aiClient, bs, &services.TasksService{}, store.GetDB())

	tasks, err := openWATasks(ctx, *email, *limit)
	if err != nil {
		log.Fatalf("failed to list open WhatsApp tasks: %v", err)
	}
	fmt.Printf("found %d open WhatsApp task(s) for %s\n", len(tasks), *email)

	for _, task := range tasks {
		reassessTask(ctx, *email, completionSvc, bs, task)
	}

	bs.printResults()
	if *apply {
		fmt.Printf("\nwrote %d confirm-first candidate(s)\n", bs.written)
	} else {
		fmt.Println("\ndry run: no writes (pass -apply to record confirm-first candidates)")
	}
}

func providerConfig(cfg *config.Config) ai.ProviderConfig {
	return ai.ProviderConfig{
		Provider:                 cfg.AIProvider,
		GeminiAPIKey:             cfg.GeminiAPIKey,
		GeminiAnalysisModel:      cfg.GeminiAnalysisModel,
		GeminiTranslationModel:   cfg.GeminiTranslationModel,
		DeepSeekAPIKey:           cfg.DeepSeekAPIKey,
		DeepSeekBaseURL:          cfg.DeepSeekBaseURL,
		DeepSeekFilterModel:      cfg.DeepSeekFilterModel,
		DeepSeekAnalysisModel:    cfg.DeepSeekAnalysisModel,
		DeepSeekTranslationModel: cfg.DeepSeekTranslationModel,
		DeepSeekReportModel:      cfg.DeepSeekReportModel,
	}
}

// statusChatName is WhatsApp's own status-broadcast pseudo-chat -- never a real
// conversation, so it is excluded both as a task's room and as a reply's chat.
const statusChatName = "status"

// openWATasks returns the user's open (not done) WhatsApp tasks, most recent first,
// capped at limit.
func openWATasks(ctx context.Context, email string, limit int) ([]store.ConsolidatedMessage, error) {
	all, err := store.GetMessages(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("GetMessages: %w", err)
	}
	var out []store.ConsolidatedMessage
	for _, m := range all {
		if m.Source != store.SourceWhatsApp || m.Done || m.IsDeleted || m.Room == statusChatName {
			continue
		}
		out = append(out, m)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// rawMessagePayload is the subset of types.RawMessage decoded from wa_messages.raw_json
// needed to match a reply against the thread it was sent in reply to.
type rawMessagePayload struct {
	ReplyToID string
}

// candidateReplyFetcher is a seam over store.ListWAMessagesForChatSince so tests can
// inject fake chat histories without a DB round trip.
var candidateReplyFetcher = store.ListWAMessagesForChatSince

// reassessTask fetches the task's chat history after AssignedAt, selects up to
// replyCandidateCap candidate replies by priority, and evaluates each against the
// task with the same evaluator the live sweep uses, stopping at the first RESOLVE.
func reassessTask(ctx context.Context, email string, completionSvc *services.CompletionService, bs *backlogStore, task store.ConsolidatedMessage) {
	replies, err := candidateReplyFetcher(ctx, email, task.Room, task.AssignedAt.Unix())
	if err != nil {
		fmt.Printf("skip task %d: fetch chat history failed: %v\n", task.ID, err)
		return
	}

	candidates := selectCandidateReplies(task, replies)
	for _, reply := range candidates {
		evaluateReplyAgainstTask(ctx, completionSvc, bs, task, reply)
		if bs.isResolved(task.ID) {
			break
		}
	}
}

// selectCandidateReplies picks up to replyCandidateCap wa_messages rows to evaluate
// against task, in priority order: (1) raw_json ReplyToID pointing back to the task's
// thread/source/reply anchor, (2) sender is the task's assignee or requester,
// (3) the body carries a completion signal, (4) the body shares >=2 topical tokens
// with the task title. Skips the task's own source message, empty bodies, and the
// status pseudo-chat.
func selectCandidateReplies(task store.ConsolidatedMessage, replies []store.WAChatMessage) []store.WAChatMessage {
	titleTokens := topicalTokens(task.Task)
	var tiers [4][]store.WAChatMessage
	for _, r := range replies {
		if r.MessageID == task.SourceTS || strings.TrimSpace(r.Body) == "" {
			continue
		}
		switch {
		case repliesToTask(r, task):
			tiers[0] = append(tiers[0], r)
		case isAssigneeOrRequester(r.Sender, task):
			tiers[1] = append(tiers[1], r)
		case services.HasCompletionSignal(r.Body):
			tiers[2] = append(tiers[2], r)
		case sharesTopicalTokens(titleTokens, r.Body):
			tiers[3] = append(tiers[3], r)
		}
	}

	var out []store.WAChatMessage
	for _, tier := range tiers {
		for _, r := range tier {
			if len(out) >= replyCandidateCap {
				return out
			}
			out = append(out, r)
		}
	}
	return out
}

// repliesToTask reports whether r's raw_json ReplyToID anchors it to the task's
// thread ID, source message, or replied-to ID.
func repliesToTask(r store.WAChatMessage, task store.ConsolidatedMessage) bool {
	if r.RawJSON == "" {
		return false
	}
	var payload rawMessagePayload
	if err := json.Unmarshal([]byte(r.RawJSON), &payload); err != nil || payload.ReplyToID == "" {
		return false
	}
	return payload.ReplyToID == task.ThreadID || payload.ReplyToID == task.SourceTS || payload.ReplyToID == task.RepliedToID
}

// isAssigneeOrRequester reports whether sender identifies the same person as the
// task's assignee or requester, tolerating case, whitespace, and the "(Ambiguous)"
// report-time suffix.
func isAssigneeOrRequester(sender string, task store.ConsolidatedMessage) bool {
	s := normalizeIdentity(sender)
	if s == "" {
		return false
	}
	return s == normalizeIdentity(task.Assignee) || s == normalizeIdentity(task.Requester)
}

// normalizeIdentity mirrors services.normalizeSenderIdentity (unexported): lowercases,
// strips the "(Ambiguous)" report-time suffix, and collapses whitespace so two display
// names can be compared exactly.
func normalizeIdentity(raw string) string {
	name := strings.TrimSuffix(strings.TrimSpace(raw), "(Ambiguous)")
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.Join(strings.Fields(name), " ")
}

// topicalTokens mirrors services.ftsCandidateTokens (unexported): lowercase tokens of
// >=3 runes, split on non-letter/digit boundaries.
func topicalTokens(text string) map[string]bool {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		if len([]rune(f)) < 3 {
			continue
		}
		out[strings.ToLower(f)] = true
	}
	return out
}

// sharesTopicalTokens reports whether body shares at least 2 distinct tokens with
// titleTokens, mirroring the minTopicalOverlap floor services uses for cross-thread
// candidate gating.
func sharesTopicalTokens(titleTokens map[string]bool, body string) bool {
	const minOverlap = 2
	hits := 0
	seen := map[string]bool{}
	for token := range topicalTokens(body) {
		if !titleTokens[token] || seen[token] {
			continue
		}
		seen[token] = true
		hits++
		if hits >= minOverlap {
			return true
		}
	}
	return false
}

// evaluateReplyAgainstTask runs the SAME evaluator as the sweep (EvaluateThreadReply)
// for one (task, reply) pair. bs.currentTask/currentSender are set first so the
// backlogStore's write interception can check the task's own dismissal metadata and
// record the actual reply speaker for the audit table.
func evaluateReplyAgainstTask(ctx context.Context, completionSvc *services.CompletionService, bs *backlogStore, task store.ConsolidatedMessage, reply store.WAChatMessage) {
	env := store.ConsolidatedMessage{
		UserEmail:    task.UserEmail,
		Source:       store.SourceWhatsApp,
		Room:         task.Room,
		Requester:    reply.Sender,
		OriginalText: reply.Body,
		SourceTS:     reply.MessageID,
	}
	bs.currentTask = task
	bs.currentSender = reply.Sender
	bs.currentReplyTS = reply.TS
	if _, err := completionSvc.EvaluateThreadReply(ctx, env, []store.ConsolidatedMessage{task}); err != nil {
		fmt.Printf("evaluate task %d reply %s: %v\n", task.ID, reply.MessageID, err)
	}
}

func truncateForDisplay(s string, max int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max])
}

// resultRow is one printed audit line: one (task, reply) pair the evaluator judged.
type resultRow struct {
	taskID  store.MessageID
	room    string
	verdict string
	speaker string
	when    time.Time
	quote   string
}

// backlogStore wraps the real DefaultTaskStore so EvaluateThreadReply's writes never
// hard-close or auto-update a task from this tool: every actionable verdict is
// recorded for the audit table and, only under -apply, persisted as a confirm-first
// candidate (never the real resolve/update).
type backlogStore struct {
	inner services.TaskStore
	db    *sql.DB
	apply bool

	mu             sync.Mutex
	rows           []resultRow
	resolved       map[store.MessageID]bool
	written        int
	currentTask    store.ConsolidatedMessage
	currentSender  string
	currentReplyTS int64
}

func (b *backlogStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return b.inner.GetIncompleteByThreadID(ctx, q, email, threadID)
}

func (b *backlogStore) HasAnyTaskInThread(ctx context.Context, q store.Querier, email, threadID string) (bool, error) {
	return b.inner.HasAnyTaskInThread(ctx, q, email, threadID)
}

func (b *backlogStore) GetLatestThreadAssignee(ctx context.Context, q store.Querier, email, threadID string) (string, error) {
	return b.inner.GetLatestThreadAssignee(ctx, q, email, threadID)
}

func (b *backlogStore) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error {
	return b.inner.UpdateMessageCategory(ctx, q, email, id, category)
}

// UpdateSubtasks is a no-op: this tool never applies subtask cascades, only records
// confirm-first candidates.
func (b *backlogStore) UpdateSubtasks(ctx context.Context, q store.Querier, email string, id store.MessageID, subtasks []store.Subtask) error {
	return nil
}

func (b *backlogStore) GetRecentIncompleteGmail(ctx context.Context, q store.Querier, email string) ([]store.ConsolidatedMessage, error) {
	return b.inner.GetRecentIncompleteGmail(ctx, q, email)
}

func (b *backlogStore) SearchOpenTasksFTS(ctx context.Context, email string, tokens []string, limit int) ([]store.ConsolidatedMessage, error) {
	return b.inner.SearchOpenTasksFTS(ctx, email, tokens, limit)
}

// HandleTaskState intercepts every RESOLVE (from the task's own assignee) and UPDATE
// verdict EvaluateThreadReply would otherwise apply directly. It never performs the
// real resolve/update; under -apply it downgrades the verdict to a confirm-first
// candidate instead, respecting any prior dismissal of the same source.
func (b *backlogStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	verdict := strings.ToUpper(item.State)
	var id store.MessageID
	if item.ID != nil {
		id = *item.ID
	}
	b.record(id, msg.Room, verdict+" (downgraded to candidate)", b.currentSender, msg.OriginalText)
	if !b.apply || item.ID == nil {
		return 0, nil
	}
	sourceKey := msg.Link
	if sourceKey == "" {
		sourceKey = msg.SourceTS
	}
	if store.WasCandidateDismissed(string(b.currentTask.Metadata), sourceKey) {
		return 0, nil
	}
	cand := store.CompletionCandidate{
		SourceLink: sourceKey,
		SourceText: truncateForDisplay(msg.OriginalText, 280),
		Evidence:   fmt.Sprintf("whatsapp backlog reassessment (%s)", verdict),
		DetectedAt: time.Now().UTC().Format(time.RFC3339),
		Status:     "pending",
	}
	if err := store.AddCompletionCandidate(ctx, b.db, email, *item.ID, cand); err != nil {
		return 0, err
	}
	b.mu.Lock()
	b.written++
	b.mu.Unlock()
	return *item.ID, nil
}

// AddCompletionCandidate is EvaluateThreadReply's own confirm-first path (RESOLVE from
// someone other than the assignee) -- the dismissal check already ran in the caller, so
// this only needs to gate the real write on -apply. speaker comes from currentSender:
// the store.TaskStore interface's AddCompletionCandidate does not carry the reply's
// sender, so it cannot be read off cand.
func (b *backlogStore) AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error {
	b.record(id, b.currentTask.Room, "RESOLVE (candidate)", b.currentSender, cand.SourceText)
	if !b.apply {
		return nil
	}
	if err := b.inner.AddCompletionCandidate(ctx, q, email, id, cand); err != nil {
		return err
	}
	b.mu.Lock()
	b.written++
	b.mu.Unlock()
	return nil
}

// isResolved reports whether any recorded row for id carries a RESOLVE verdict --
// used to stop evaluating further candidate replies for a task once one resolves it.
func (b *backlogStore) isResolved(id store.MessageID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.resolved[id]
}

func (b *backlogStore) record(id store.MessageID, room, verdict, speaker, quote string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	when := time.Now().UTC()
	if b.currentReplyTS > 0 {
		when = time.Unix(b.currentReplyTS, 0).UTC()
	}
	b.rows = append(b.rows, resultRow{
		taskID: id, room: room, verdict: verdict, speaker: speaker,
		when: when, quote: truncateForDisplay(quote, 80),
	})
	if strings.HasPrefix(verdict, "RESOLVE") {
		if b.resolved == nil {
			b.resolved = map[store.MessageID]bool{}
		}
		b.resolved[id] = true
	}
}

func (b *backlogStore) printResults() {
	b.mu.Lock()
	rows := append([]resultRow(nil), b.rows...)
	b.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].taskID < rows[j].taskID })
	fmt.Printf("\n%-8s %-20s %-32s %-20s %-12s %s\n", "id", "room", "verdict", "speaker", "when", "quote")
	for _, r := range rows {
		fmt.Printf("%-8d %-20s %-32s %-20s %-12s %s\n", r.taskID, r.room, r.verdict, r.speaker, r.when.Format("2006-01-02"), r.quote)
	}
	if len(rows) == 0 {
		fmt.Println("(no actionable verdicts found)")
	}
}
