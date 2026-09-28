package scanner

import (
	"context"
	"fmt"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"time"
)

// slackAdapter feeds one user's per-channel candidate batches (already
// classified in the drain phase) through the shared channel driver.
type slackAdapter struct {
	// ctx is the scan-scoped context; BuildPayload/Mentions resolve user names
	// through the Slack API and the ChannelAdapter interface carries no ctx.
	ctx      context.Context
	sc       *channels.SlackClient
	buf      map[string][]types.RawMessage // channelName → msgs
	rooms    map[string]string             // channelName → channelID
	chanOf   map[string]string             // messageID → channelID, for AckScanned
	tracker  *slackHeldTracker
	consumed bool
}

func newSlackAdapter(ctx context.Context, sc *channels.SlackClient, byChannel map[string][]types.RawMessage, tracker *slackHeldTracker) *slackAdapter {
	buf := make(map[string][]types.RawMessage, len(byChannel))
	rooms := make(map[string]string, len(byChannel))
	chanOf := make(map[string]string, len(byChannel))
	for channelID, msgs := range byChannel {
		name := sc.GetChannelName(channelID)
		buf[name] = append(buf[name], msgs...)
		rooms[name] = channelID
		for _, m := range msgs {
			chanOf[m.ID] = channelID
		}
	}
	return &slackAdapter{ctx: ctx, sc: sc, buf: buf, rooms: rooms, chanOf: chanOf, tracker: tracker}
}

// AckScanned records which channelID a failed group's messages belong to, via
// slackHeldTracker, so scanSlack can withhold that channel's cursor advance instead
// of masking unanalyzed history as processed. ok=true is a no-op: the drain-phase
// cursor advance in classifyAndCollect already covers this case.
func (a *slackAdapter) AckScanned(_ context.Context, email string, ids []string, ok bool) {
	if ok || a.tracker == nil {
		return
	}
	for _, id := range ids {
		if chanID, found := a.chanOf[id]; found {
			a.tracker.hold(email, chanID)
		}
	}
}

func (a *slackAdapter) Source() string    { return store.SourceSlack }
func (a *slackAdapter) LogPrefix() string { return "SLACK" }

func (a *slackAdapter) PopMessages(context.Context, string) map[string][]types.RawMessage {
	if a.consumed {
		return nil
	}
	a.consumed = true
	return a.buf
}

func (a *slackAdapter) GetGroupName(_, roomKey string) string { return roomKey }

// Is1To1 — Slack DM channel IDs carry the "D" prefix.
func (a *slackAdapter) Is1To1(roomKey string) bool {
	return strings.HasPrefix(a.rooms[roomKey], "D")
}

func (a *slackAdapter) BuildPayload(_ store.User, _ []string, msgs []types.RawMessage) (string, map[string]types.RawMessage) {
	return buildSlackAnalysisPayload(a.ctx, msgs, a.sc)
}

func (a *slackAdapter) Enrich(roomKey, payload string, ts time.Time) (*types.EnrichedMessage, error) {
	var last types.RawMessage
	if msgs := a.buf[roomKey]; len(msgs) > 0 {
		last = msgs[len(msgs)-1]
	}
	senderName := last.SenderName
	if senderName == "" {
		senderName = last.Sender
	}
	return EnrichSlackMessage(last.Sender, senderName, last.ChannelID, last.ReplyToID, payload, ts)
}

// IsFromMe — always false toward the driver: Slack owns its fromMe behaviors in
// the drain phase (completion dispatch in classifyAndCollect) and historically
// never applied the driver's fromMe-in-group category override.
func (a *slackAdapter) IsFromMe(types.RawMessage, store.User) bool { return false }

// IsOwnMessage — the real sender-identity check, kept separate from IsFromMe so
// the category-override path (saveChannelItem) stays false while the
// resolve-trust path (candidate injection) still sees the user's own messages.
func (a *slackAdapter) IsOwnMessage(m types.RawMessage, user store.User) bool {
	return isFromUser(&user, m)
}

func (a *slackAdapter) Mentions(m types.RawMessage) []string {
	return resolveSlackMentionNames(a.ctx, a.sc, extractSlackMentionUserIDs(m.Text))
}

// ownsCompletionDispatch — dispatchOutgoingCompletionIfMine already covers ALL
// raw rows pre-classification; the driver must not re-dispatch the classified subset.
func (a *slackAdapter) ownsCompletionDispatch() {}

// SaveThreadID — replies anchor on the parent thread ts, root messages on their own ID.
func (a *slackAdapter) SaveThreadID(m types.RawMessage) string { return slackThreadTS(m) }

// ProposalThreadID — Slack RawMessages never populate ThreadID (only ReplyToID on
// replies), so the candidate-injection loop needs this to guard against
// cross-thread fuzzy matches. Uses the same anchor as SaveThreadID so a proposal's
// thread lines up with the thread_id already persisted on the task it may match.
func (a *slackAdapter) ProposalThreadID(m types.RawMessage) string { return slackThreadTS(m) }

func (a *slackAdapter) SaveLink(ctx context.Context, m types.RawMessage, email string) string {
	return buildSlackLinkAndRegisterThread(ctx, m, email)
}

func buildSlackAnalysisPayload(ctx context.Context, candidates []types.RawMessage, sc slackUserResolver) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range candidates {
		msgMap[m.ID] = m
		resolvedText := resolveSlackMentions(ctx, m.Text, sc)
		metaStr := buildSlackMetadataString(m)
		senderLabel := m.SenderName
		if senderLabel == "" {
			senderLabel = m.Sender
		}
		tsTag := formatTSTag(m.Timestamp)
		writePayloadLine(&sb, m.ID, tsTag, metaStr, senderLabel, resolvedText)
	}
	return sb.String(), msgMap
}

func buildSlackMetadataString(m types.RawMessage) string {
	var tags []string
	if m.IsPinned {
		tags = append(tags, "Pinned")
	}
	if m.IsImportant {
		tags = append(tags, "Important")
	}
	if m.IsForwarded {
		tags = append(tags, "Forwarded")
	}
	return formatTagBlock(tags) +
		formatListBlock("Reactions", m.Reactions) +
		formatListBlock("Files", m.AttachmentNames)
}

func buildSlackLink(m types.RawMessage) string {
	link := fmt.Sprintf("https://slack.com/archives/%s/p%s", m.ChannelID, strings.ReplaceAll(m.ID, ".", ""))
	if m.ReplyToID != "" {
		link += fmt.Sprintf("?thread_ts=%s", m.ReplyToID)
	}
	return link
}

func slackThreadTS(m types.RawMessage) string {
	if m.ReplyToID != "" {
		return m.ReplyToID
	}
	return m.ID
}

func buildSlackLinkAndRegisterThread(ctx context.Context, m types.RawMessage, email string) string {
	link := buildSlackLink(m)
	threadTS := slackThreadTS(m)
	// Why: register both parent messages and replies so slow sweeper always tracks future activity.
	if err := store.RegisterTargetedSlackThread(ctx, m.ChannelID, threadTS, m.ID, email); err == nil {
		logger.Debugf("[SLACK] thread registered for tracking: %s (user: %s)", threadTS, email)
	}
	return link
}
