package scanner

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"sync"

	"github.com/slack-go/slack"
)

// maxThreadReplyEvalsPerSweep bounds how many LLM transition calls one sweepSlackThreads
// invocation may spend evaluating plain (non-keyword) counterparty thread replies.
// Why: production evidence showed every real completion was a same-thread reply, so
// dropping the keyword gate now sends every counterparty reply in a tracked thread to
// EvaluateThreadReply -- this caps the blast radius of one sweep tick.
const maxThreadReplyEvalsPerSweep = 29

// threadReplyBudget shares an LLM-call counter across one sweepSlackThreads run (hot
// and cold tiers alike) so the cap applies per sweep, not per thread group.
type threadReplyBudget struct {
	mu   sync.Mutex
	used int
}

func newThreadReplyBudget() *threadReplyBudget { return &threadReplyBudget{} }

// take claims one slot from the budget, returning false once the cap is reached.
func (b *threadReplyBudget) take() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= maxThreadReplyEvalsPerSweep {
		return false
	}
	b.used++
	return true
}

// BuildThreadCompletionEnvelope builds the ConsolidatedMessage envelope EvaluateThreadReply
// evaluates for one Slack thread reply. Exported so cmd/reassess-slack-backlog reuses the
// exact same builder the sweep uses instead of maintaining a second, divergent one.
//
// Why: the live-scan sibling (dispatchSlackThreadedCompletion) propagates the full
// envelope; the sweeper left Room empty and validateTargetTask rejects a blank Room
// as a cross-room operation, so every sweeper-side completion was dropped.
func BuildThreadCompletionEnvelope(user *store.User, t store.SlackThreadMeta, m slack.Message, room, senderName string, fromMe bool) store.ConsolidatedMessage {
	ts := channels.ParseSlackTimestamp(m.Timestamp)
	env := store.ConsolidatedMessage{
		UserEmail: user.Email, Source: store.SourceSlack,
		Room:           room,
		Link:           buildSlackLink(types.RawMessage{ID: m.Timestamp, ChannelID: t.ChannelID, ReplyToID: t.ThreadTS}),
		Requester:      senderName,
		AssignedAt:     ts,
		CreatedAt:      ts,
		ThreadID:       t.ThreadTS,
		RepliedToID:    t.ThreadTS,
		OriginalText:   m.Text,
		SourceTS:       m.Timestamp,
		SourceChannels: []string{store.SourceSlack},
	}
	if fromMe {
		env.RequesterCanonical = user.Email
	}
	return env
}

func dispatchThreadCompletionIfMine(ctx context.Context, sc *channels.SlackClient, user *store.User, t store.SlackThreadMeta, m slack.Message, budget *threadReplyBudget) {
	if deps.completionSvc == nil || m.ThreadTimestamp == "" {
		return
	}
	senderName := sc.GetUserName(ctx, m.User)
	room := sc.GetChannelName(t.ChannelID)
	if strings.EqualFold(m.User, user.SlackID) || senderName == user.Name {
		env := BuildThreadCompletionEnvelope(user, t, m, room, senderName, true)
		if _, err := deps.completionSvc.ProcessPotentialCompletion(ctx, env); err != nil {
			logger.Warnf("[SLACK] thread completion failed for %s: %v", user.Email, err)
		}
		return
	}
	dispatchCounterpartyThreadReply(ctx, user, t, m, room, senderName, budget)
}

// dispatchCounterpartyThreadReply handles a reply from someone other than the tracked
// user. Why: production evidence showed every real completion was a plain same-thread
// reply (an ack or an answer) that never used explicit completion wording, so the
// keyword gate below only ever saw the minority of cases. When the thread has an open
// task of its own, EvaluateThreadReply judges the reply directly, without the gate; the
// keyword-gated cross-channel path stays as the fallback for threads with no open task.
func dispatchCounterpartyThreadReply(ctx context.Context, user *store.User, t store.SlackThreadMeta, m slack.Message, room, senderName string, budget *threadReplyBudget) {
	env := BuildThreadCompletionEnvelope(user, t, m, room, senderName, false)
	tasks, err := store.GetIncompleteByThreadID(ctx, store.GetDB(), user.Email, t.ThreadTS)
	if err != nil {
		logger.Warnf("[SLACK] thread reply task lookup failed for %s: %v", user.Email, err)
		return
	}
	if len(tasks) == 0 {
		if services.HasCompletionSignal(m.Text) {
			if _, err := deps.completionSvc.ProcessCrossChannelSignal(ctx, env); err != nil {
				logger.Warnf("[SLACK] thread cross-channel completion failed for %s: %v", user.Email, err)
			}
		}
		return
	}
	if !budget.take() {
		logger.Warnf("[SLACK] thread reply evaluation capped at %d for this sweep; skipping thread=%s", maxThreadReplyEvalsPerSweep, t.ThreadTS)
		return
	}
	if _, err := deps.completionSvc.EvaluateThreadReply(ctx, env, tasks); err != nil {
		logger.Warnf("[SLACK] thread reply evaluation failed for %s: %v", user.Email, err)
	}
}
