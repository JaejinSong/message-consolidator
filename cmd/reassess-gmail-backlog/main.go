// Command reassess-gmail-backlog re-runs the Gmail thread-reply evaluator (see
// services.CompletionService.EvaluateThreadReply) against a user's already-open
// Gmail tasks, fetching each task's full Gmail thread directly from the Gmail API
// instead of waiting for the next sweep tick. Dry run (default) only prints what it
// would do; -apply writes confirm-first candidates only -- it never hard-closes a
// task, even when the verdict is a RESOLVE from the task's own assignee.
//
// Usage: go run ./cmd/reassess-gmail-backlog -email user@example.com [-apply] [-limit 97]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/mail"
	"sort"
	"strings"

	"message-consolidator/channels"
	"message-consolidator/cmd/internal/backlog"
	"message-consolidator/services"
	"message-consolidator/store"

	"google.golang.org/api/gmail/v1"
)

// replyCandidateCap bounds the LLM calls per task (prime).
const replyCandidateCap = 13

func main() {
	email := flag.String("email", "", "user email whose open Gmail tasks should be reassessed")
	apply := flag.Bool("apply", false, "write confirm-first candidates (default: dry run, no writes)")
	limit := flag.Int("limit", 97, "max number of open Gmail tasks to reassess")
	flag.Int64Var(&dumpTaskID, "dump-task", 0, "print the full cleaned body of every candidate reply for this task ID (audit aid)")
	flag.Parse()

	if *email == "" {
		log.Fatal("usage: reassess-gmail-backlog -email <user@example.com> [-apply] [-limit N]")
	}

	ctx := context.Background()
	env, err := backlog.Bootstrap(ctx, "gmail", *apply)
	if err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	channels.SetupGmailOAuth(env.Cfg)
	svc, err := channels.GetGmailService(ctx, *email)
	if err != nil {
		log.Fatalf("Gmail client init failed for %s: %v", *email, err)
	}

	tasks, err := openGmailTasks(ctx, *email, *limit)
	if err != nil {
		log.Fatalf("failed to list open Gmail tasks: %v", err)
	}
	fmt.Printf("found %d open Gmail task(s) for %s\n", len(tasks), *email)

	for _, group := range groupByGmailThread(tasks) {
		reassessGmailThread(ctx, svc, env.CompletionSvc, env.Store, group)
	}

	env.Store.Finish(*apply)
}

// openGmailTasks returns the user's open (lifecycle active) Gmail tasks that carry a
// thread ID, most recent first, capped at limit.
func openGmailTasks(ctx context.Context, email string, limit int) ([]store.ConsolidatedMessage, error) {
	all, err := store.GetMessages(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("GetMessages: %w", err)
	}
	var out []store.ConsolidatedMessage
	for _, m := range all {
		if m.Source != store.SourceGmail || !m.IsActive() || m.ThreadID == "" {
			continue
		}
		out = append(out, m)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// gmailTaskGroup is every still-open Gmail task sharing one Gmail thread.
type gmailTaskGroup struct {
	threadID string
	tasks    []store.ConsolidatedMessage
}

// groupByGmailThread buckets tasks sharing a thread ID so the thread is fetched once.
func groupByGmailThread(tasks []store.ConsolidatedMessage) []gmailTaskGroup {
	index := map[string]int{}
	var groups []gmailTaskGroup
	for _, t := range tasks {
		if i, ok := index[t.ThreadID]; ok {
			groups[i].tasks = append(groups[i].tasks, t)
			continue
		}
		index[t.ThreadID] = len(groups)
		groups = append(groups, gmailTaskGroup{threadID: t.ThreadID, tasks: []store.ConsolidatedMessage{t}})
	}
	return groups
}

// fetchGmailThread is a seam over the real Gmail API call so tests can inject fake
// thread histories without a network round trip.
// dumpTaskID is set by -dump-task; package-level so the evaluator can print without threading a flag through every call.
var dumpTaskID int64

var fetchGmailThread = defaultFetchGmailThread

func defaultFetchGmailThread(ctx context.Context, svc *gmail.Service, threadID string) (*gmail.Thread, error) {
	return svc.Users.Threads.Get("me", threadID).Format("full").Context(ctx).Do()
}

// reassessGmailThread fetches one Gmail thread's full message history and evaluates
// candidate replies against every task in the group.
func reassessGmailThread(ctx context.Context, svc *gmail.Service, completionSvc *services.CompletionService, bs *backlog.Store, group gmailTaskGroup) {
	thread, err := fetchGmailThread(ctx, svc, group.threadID)
	if err != nil {
		fmt.Printf("skip thread %s: fetch failed: %v\n", group.threadID, err)
		return
	}
	for _, task := range group.tasks {
		reassessGmailTask(ctx, completionSvc, bs, task, thread.Messages)
	}
}

// reassessGmailTask selects up to replyCandidateCap candidate replies for task and
// evaluates each against the task with the same evaluator the live sweep uses,
// stopping at the first RESOLVE.
func reassessGmailTask(ctx context.Context, completionSvc *services.CompletionService, bs *backlog.Store, task store.ConsolidatedMessage, messages []*gmail.Message) {
	sourceMsgID := strings.TrimPrefix(task.SourceTS, "gmail-")
	candidates := gmailReplyCandidates(sourceMsgID, messages)
	if len(candidates) == 0 {
		fmt.Printf("skip task %d: no candidate replies after source message %s\n", task.ID, sourceMsgID)
		return
	}
	for _, m := range candidates {
		evaluateGmailReplyAgainstTask(ctx, completionSvc, bs, task, m)
		if bs.IsResolved(task.ID) {
			break
		}
	}
}

// gmailReplyCandidates picks up to replyCandidateCap thread messages strictly after
// the message identified by sourceMsgID (by internal date), excluding the source
// message itself. When more than the cap qualify, the most recent ones are kept, in
// chronological (newest-last) order. Returns nil when sourceMsgID is not found in
// messages.
func gmailReplyCandidates(sourceMsgID string, messages []*gmail.Message) []*gmail.Message {
	sourceTS, found := gmailInternalDate(sourceMsgID, messages)
	if !found {
		return nil
	}

	var after []*gmail.Message
	for _, m := range messages {
		if m.Id == sourceMsgID || m.InternalDate <= sourceTS {
			continue
		}
		after = append(after, m)
	}
	sort.Slice(after, func(i, j int) bool { return after[i].InternalDate < after[j].InternalDate })
	if len(after) > replyCandidateCap {
		after = after[len(after)-replyCandidateCap:]
	}
	return after
}

func gmailInternalDate(msgID string, messages []*gmail.Message) (int64, bool) {
	for _, m := range messages {
		if m.Id == msgID {
			return m.InternalDate, true
		}
	}
	return 0, false
}

// evaluateGmailReplyAgainstTask runs the SAME evaluator as the sweep
// (EvaluateThreadReply) for one (task, reply) pair, building the reply text the same
// way the Gmail scanner builds a message's body (channels.ExtractCleanBody) so quoted
// history is stripped the same way. bs.CurrentTask/CurrentSender/CurrentReplyTS are
// set first so the backlog.Store's write interception can check the task's own
// dismissal metadata and record the actual reply speaker and timestamp for the audit
// table.
func evaluateGmailReplyAgainstTask(ctx context.Context, completionSvc *services.CompletionService, bs *backlog.Store, task store.ConsolidatedMessage, m *gmail.Message) {
	if m.Payload == nil {
		return
	}
	body := channels.ExtractCleanBody(m.Payload)
	if strings.TrimSpace(body) == "" {
		return
	}
	sender := gmailSenderDisplayName(m.Payload)
	if dumpTaskID != 0 && int64(task.ID) == dumpTaskID {
		fmt.Printf("---- task %d reply %s from %s ----\n%s\n", task.ID, m.Id, sender, body)
	}

	env := store.ConsolidatedMessage{
		UserEmail:    task.UserEmail,
		Source:       store.SourceGmail,
		Room:         task.Room,
		ThreadID:     task.ThreadID,
		Requester:    sender,
		OriginalText: body,
		SourceTS:     fmt.Sprintf("gmail-%s", m.Id),
	}
	bs.CurrentTask = task
	bs.CurrentSender = sender
	bs.CurrentReplyTS = m.InternalDate / 1000
	if _, err := completionSvc.EvaluateThreadReply(ctx, env, []store.ConsolidatedMessage{task}); err != nil {
		fmt.Printf("evaluate task %d reply %s: %v\n", task.ID, m.Id, err)
	}
}

// gmailSenderDisplayName reads the reply's From header display name, falling back to
// the bare address, then to the raw header value when it does not parse.
func gmailSenderDisplayName(payload *gmail.MessagePart) string {
	for _, h := range payload.Headers {
		if h.Name != "From" {
			continue
		}
		addr, err := mail.ParseAddress(h.Value)
		if err != nil {
			return h.Value
		}
		if addr.Name != "" {
			return addr.Name
		}
		return addr.Address
	}
	return ""
}
