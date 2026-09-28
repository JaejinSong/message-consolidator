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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"unicode"

	"message-consolidator/cmd/internal/backlog"
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
	env, err := backlog.Bootstrap(ctx, "whatsapp", *apply)
	if err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	tasks, err := openWATasks(ctx, *email, *limit)
	if err != nil {
		log.Fatalf("failed to list open WhatsApp tasks: %v", err)
	}
	fmt.Printf("found %d open WhatsApp task(s) for %s\n", len(tasks), *email)

	for _, task := range tasks {
		reassessTask(ctx, *email, env.CompletionSvc, env.Store, task)
	}

	env.Store.Finish(*apply)
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
func reassessTask(ctx context.Context, email string, completionSvc *services.CompletionService, bs *backlog.Store, task store.ConsolidatedMessage) {
	replies, err := candidateReplyFetcher(ctx, email, task.Room, task.AssignedAt.Unix())
	if err != nil {
		fmt.Printf("skip task %d: fetch chat history failed: %v\n", task.ID, err)
		return
	}

	candidates := selectCandidateReplies(task, replies)
	for _, reply := range candidates {
		evaluateReplyAgainstTask(ctx, completionSvc, bs, task, reply)
		if bs.IsResolved(task.ID) {
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
		if tier := replyTier(task, titleTokens, r); tier >= 0 {
			tiers[tier] = append(tiers[tier], r)
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

// replyTier returns r's priority tier (0 = highest) or -1 when r is not a candidate.
func replyTier(task store.ConsolidatedMessage, titleTokens map[string]bool, r store.WAChatMessage) int {
	if r.MessageID == task.SourceTS || strings.TrimSpace(r.Body) == "" {
		return -1
	}
	if repliesToTask(r, task) {
		return 0
	}
	// Why: a group-chat line without a quote anchor is judged against the title alone, so a bare "thanks" or unrelated file from the requester read as delivery (2026-09-28 dry-run: 4/5 false RESOLVE).
	if !sharesTopicalTokens(titleTokens, r.Body) {
		return -1
	}
	switch {
	case isAssigneeOrRequester(r.Sender, task):
		return 1
	case services.HasCompletionSignal(r.Body):
		return 2
	}
	return 3
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
	s := services.NormalizeSenderIdentity(sender)
	if s == "" {
		return false
	}
	return s == services.NormalizeSenderIdentity(task.Assignee) || s == services.NormalizeSenderIdentity(task.Requester)
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
// for one (task, reply) pair. bs.CurrentTask/CurrentSender/CurrentReplyTS are set first
// so the backlog.Store's write interception can check the task's own dismissal metadata
// and record the actual reply speaker and timestamp for the audit table.
func evaluateReplyAgainstTask(ctx context.Context, completionSvc *services.CompletionService, bs *backlog.Store, task store.ConsolidatedMessage, reply store.WAChatMessage) {
	env := store.ConsolidatedMessage{
		UserEmail:    task.UserEmail,
		Source:       store.SourceWhatsApp,
		Room:         task.Room,
		Requester:    reply.Sender,
		OriginalText: reply.Body,
		SourceTS:     reply.MessageID,
	}
	bs.CurrentTask = task
	bs.CurrentSender = reply.Sender
	bs.CurrentReplyTS = reply.TS
	if _, err := completionSvc.EvaluateThreadReply(ctx, env, []store.ConsolidatedMessage{task}); err != nil {
		fmt.Printf("evaluate task %d reply %s: %v\n", task.ID, reply.MessageID, err)
	}
}
