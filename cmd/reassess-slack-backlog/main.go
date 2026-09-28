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
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"message-consolidator/channels"
	"message-consolidator/cmd/internal/backlog"
	"message-consolidator/config"
	"message-consolidator/scanner"
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

	env, err := backlog.Bootstrap(ctx, "slack", *apply)
	if err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	sc, clientKind := slackClientForEmail(ctx, env.Cfg, *email)
	fmt.Printf("using %s Slack client for %s\n", clientKind, *email)

	tasks, err := openSlackTasks(ctx, *email, *limit)
	if err != nil {
		log.Fatalf("failed to list open Slack tasks: %v", err)
	}
	fmt.Printf("found %d open Slack task(s) with a thread for %s\n", len(tasks), *email)

	for _, group := range groupByThread(tasks) {
		reassessThread(ctx, sc, env.CompletionSvc, env.Store, group)
	}

	env.Store.Finish(*apply)
}

// slackClientForEmail prefers email's own Slack OAuth grant over the bot token, mirroring
// the scanner's user-token-first scan order. Why: reassessment against the user's own
// channel membership sees threads the bot may no longer be a member of.
func slackClientForEmail(ctx context.Context, cfg *config.Config, email string) (*channels.SlackClient, string) {
	if tok, ok, err := store.GetSlackUserToken(ctx, email); err == nil && ok {
		return channels.NewSlackClient(tok.Token), "user token" //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.
	}
	return channels.NewSlackClient(cfg.SlackToken), "bot token" //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.
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
func reassessThread(ctx context.Context, sc *channels.SlackClient, completionSvc *services.CompletionService, bs *backlog.Store, group slackTaskGroup) {
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
			evaluateReplyAgainstTask(ctx, completionSvc, bs, group, m, task, room, senderName, replyTime)
		}
	}
}

// buildToolEnvelope builds the reply envelope EvaluateThreadReply judges, delegating to
// scanner.BuildThreadCompletionEnvelope -- the SAME builder the sweep uses -- so the tool
// and the sweep can never silently diverge on how Requester/Room/Link get populated.
func buildToolEnvelope(group slackTaskGroup, task store.ConsolidatedMessage, m slack.Message, room, senderName string) store.ConsolidatedMessage {
	meta := store.SlackThreadMeta{UserEmail: task.UserEmail, ChannelID: group.channelID, ThreadTS: group.threadTS}
	user := &store.User{Email: task.UserEmail}
	// Why: fromMe=false mirrors the tool's existing scope -- it always evaluates
	// every non-bot reply as a counterparty reply via EvaluateThreadReply, never
	// routing self-replies through ProcessPotentialCompletion like the sweep does.
	return scanner.BuildThreadCompletionEnvelope(user, meta, m, room, senderName, false)
}

// evaluateReplyAgainstTask runs the SAME evaluator as the sweep (EvaluateThreadReply)
// for one (task, reply) pair. bs.CurrentTask/CurrentSender/CurrentReplyTS are set first
// so the backlog.Store's write interception can check the task's own dismissal metadata
// and record the actual reply speaker and timestamp for the audit table.
func evaluateReplyAgainstTask(ctx context.Context, completionSvc *services.CompletionService, bs *backlog.Store, group slackTaskGroup, m slack.Message, task store.ConsolidatedMessage, room, senderName string, replyTime time.Time) {
	env := buildToolEnvelope(group, task, m, room, senderName)
	bs.CurrentTask = task
	bs.CurrentSender = senderName
	bs.CurrentReplyTS = replyTime.Unix()
	if _, err := completionSvc.EvaluateThreadReply(ctx, env, []store.ConsolidatedMessage{task}); err != nil {
		fmt.Printf("evaluate task %d reply %s: %v\n", task.ID, m.Timestamp, err)
	}
}
