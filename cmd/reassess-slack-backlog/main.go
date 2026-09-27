// Command reassess-slack-backlog re-runs the Slack thread-reply evaluator (see
// scanner.dispatchCounterpartyThreadReply / services.CompletionService.EvaluateThreadReply)
// against a user's already-open Slack tasks, fetching each task's full thread directly
// from Slack instead of waiting for the next sweep tick. Dry run (default) only prints
// what it would do; -apply writes confirm-first candidates only -- it never hard-closes
// a task, even when the verdict is a RESOLVE from the task's own assignee.
//
// Usage: go run ./cmd/reassess-slack-backlog -email user@example.com [-apply] [-limit 151]
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"message-consolidator/ai"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"

	"github.com/slack-go/slack"
)

func main() {
	email := flag.String("email", "", "user email whose open Slack tasks should be reassessed")
	apply := flag.Bool("apply", false, "write confirm-first candidates (default: dry run, no writes)")
	limit := flag.Int("limit", 151, "max number of open Slack tasks to reassess")
	flag.Parse()

	if *email == "" {
		log.Fatal("usage: reassess-slack-backlog -email <user@example.com> [-apply] [-limit N]")
	}

	ctx := context.Background()
	cfg := config.LoadConfig()
	if cfg.SlackToken == "" {
		log.Fatal("SLACK_TOKEN not configured")
	}
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

	sc := channels.NewSlackClient(cfg.SlackToken)
	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: *apply}
	completionSvc := services.NewCompletionService(aiClient, bs, &services.TasksService{}, store.GetDB())

	tasks, err := openSlackTasks(ctx, *email, *limit)
	if err != nil {
		log.Fatalf("failed to list open Slack tasks: %v", err)
	}
	fmt.Printf("found %d open Slack task(s) with a thread for %s\n", len(tasks), *email)

	for _, group := range groupByThread(tasks) {
		reassessThread(ctx, sc, completionSvc, bs, group)
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

// slackTaskGroup is every still-open Slack task sharing one channel+thread.
type slackTaskGroup struct {
	channelID string
	threadTS  string
	tasks     []store.ConsolidatedMessage
}

// openSlackTasks returns the user's open (not done) Slack tasks that carry a thread_id,
// most recent first, capped at limit.
func openSlackTasks(ctx context.Context, email string, limit int) ([]store.ConsolidatedMessage, error) {
	all, err := store.GetMessages(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("GetMessages: %w", err)
	}
	var out []store.ConsolidatedMessage
	for _, m := range all {
		if m.Source != store.SourceSlack || m.Done || m.IsDeleted || m.ThreadID == "" {
			continue
		}
		out = append(out, m)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// groupByThread buckets tasks sharing a channel+thread so the thread is fetched once.
// Why: a task's channel isn't stored directly -- it is recovered from the Slack
// permalink in Link (see buildSlackLink in scanner/scanner_slack.go).
func groupByThread(tasks []store.ConsolidatedMessage) []slackTaskGroup {
	type key struct{ channelID, threadTS string }
	index := map[key]int{}
	var groups []slackTaskGroup
	for _, t := range tasks {
		chID, ok := channelFromSlackLink(t.Link)
		if !ok {
			fmt.Printf("skip: task %d has no parseable Slack channel in link %q\n", t.ID, t.Link)
			continue
		}
		k := key{chID, t.ThreadID}
		if i, ok := index[k]; ok {
			groups[i].tasks = append(groups[i].tasks, t)
			continue
		}
		index[k] = len(groups)
		groups = append(groups, slackTaskGroup{channelID: chID, threadTS: t.ThreadID, tasks: []store.ConsolidatedMessage{t}})
	}
	return groups
}

// channelFromSlackLink extracts the channel ID from a buildSlackLink-shaped permalink:
// https://slack.com/archives/<channelID>/p<ts>?thread_ts=<threadTS>
func channelFromSlackLink(link string) (string, bool) {
	const prefix = "https://slack.com/archives/"
	if !strings.HasPrefix(link, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(link, prefix)
	chID, _, ok := strings.Cut(rest, "/")
	if !ok || chID == "" {
		return "", false
	}
	return chID, true
}

// slackAccessFailureReasons mirrors scanner.slackAccessFailureReasons: errors that mean
// the bot can no longer read the channel, as opposed to a transient failure.
var slackAccessFailureReasons = []string{"channel_not_found", "not_in_channel", "is_archived", "missing_scope"}

func isAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, reason := range slackAccessFailureReasons {
		if strings.Contains(msg, reason) {
			return true
		}
	}
	return false
}

// fetchThreadReplies is a seam over the real Slack call so tests can inject fake
// thread histories (and access failures) without a network round trip.
var fetchThreadReplies = defaultFetchThreadReplies

func defaultFetchThreadReplies(sc *channels.SlackClient, channelID, threadTS string) ([]slack.Message, error) {
	replies, _, _, err := sc.GetAPI().GetConversationReplies(&slack.GetConversationRepliesParameters{
		ChannelID: channelID, Timestamp: threadTS, Limit: 200,
	})
	return replies, err
}

// reassessThread fetches one thread's full reply history and evaluates every non-bot
// reply against every task in the group whose AssignedAt is before that reply.
func reassessThread(ctx context.Context, sc *channels.SlackClient, completionSvc *services.CompletionService, bs *backlogStore, group slackTaskGroup) {
	replies, err := fetchThreadReplies(sc, group.channelID, group.threadTS)
	if err != nil {
		if isAccessError(err) {
			fmt.Printf("skip channel %s: bot can no longer read it (%v)\n", group.channelID, err)
			return
		}
		fmt.Printf("skip thread %s/%s: fetch failed: %v\n", group.channelID, group.threadTS, err)
		return
	}

	for _, m := range replies {
		if m.BotID != "" || m.SubType == "bot_message" {
			continue
		}
		replyTime := channels.ParseSlackTimestamp(m.Timestamp)
		room := sc.GetChannelName(group.channelID)
		senderName := sc.GetUserName(ctx, m.User)
		for _, task := range group.tasks {
			if !replyTime.After(task.AssignedAt) {
				continue
			}
			evaluateReplyAgainstTask(ctx, completionSvc, bs, group, m, task, room, senderName)
		}
	}
}

// evaluateReplyAgainstTask runs the SAME evaluator as the sweep (EvaluateThreadReply)
// for one (task, reply) pair. bs.currentTask is set first so the backlogStore's write
// interception can check the task's own dismissal metadata.
func evaluateReplyAgainstTask(ctx context.Context, completionSvc *services.CompletionService, bs *backlogStore, group slackTaskGroup, m slack.Message, task store.ConsolidatedMessage, room, senderName string) {
	env := store.ConsolidatedMessage{
		UserEmail:    task.UserEmail,
		Source:       store.SourceSlack,
		Room:         room,
		Link:         fmt.Sprintf("https://slack.com/archives/%s/p%s?thread_ts=%s", group.channelID, strings.ReplaceAll(m.Timestamp, ".", ""), group.threadTS),
		Requester:    senderName,
		AssignedAt:   channels.ParseSlackTimestamp(m.Timestamp),
		CreatedAt:    channels.ParseSlackTimestamp(m.Timestamp),
		ThreadID:     group.threadTS,
		RepliedToID:  group.threadTS,
		OriginalText: m.Text,
		SourceTS:     m.Timestamp,
	}
	bs.currentTask = task
	if _, err := completionSvc.EvaluateThreadReply(ctx, env, []store.ConsolidatedMessage{task}); err != nil {
		fmt.Printf("evaluate task %d reply %s: %v\n", task.ID, m.Timestamp, err)
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

	mu          sync.Mutex
	rows        []resultRow
	written     int
	currentTask store.ConsolidatedMessage
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
	b.record(id, msg.Room, verdict+" (downgraded to candidate)", msg.Requester, msg.OriginalText)
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
		Evidence:   fmt.Sprintf("slack backlog reassessment (%s)", verdict),
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
// this only needs to gate the real write on -apply.
func (b *backlogStore) AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error {
	b.record(id, b.currentTask.Room, "RESOLVE (candidate)", "", cand.SourceText)
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

func (b *backlogStore) record(id store.MessageID, room, verdict, speaker, quote string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rows = append(b.rows, resultRow{taskID: id, room: room, verdict: verdict, speaker: speaker, quote: truncateForDisplay(quote, 80)})
}

func (b *backlogStore) printResults() {
	b.mu.Lock()
	rows := append([]resultRow(nil), b.rows...)
	b.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].taskID < rows[j].taskID })
	fmt.Printf("\n%-8s %-20s %-32s %-20s %s\n", "id", "room", "verdict", "speaker", "quote")
	for _, r := range rows {
		fmt.Printf("%-8d %-20s %-32s %-20s %s\n", r.taskID, r.room, r.verdict, r.speaker, r.quote)
	}
	if len(rows) == 0 {
		fmt.Println("(no actionable verdicts found)")
	}
}
