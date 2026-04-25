package channels

import (
	"errors"
	"fmt"
	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"
	"message-consolidator/types"
	"time"

	"github.com/slack-go/slack"
)

type SlackClient struct {
	api      *slack.Client
	users    map[string]slack.User
	channels map[string]string
}

func NewSlackClient(token string) *SlackClient {
	// Why: slack.OptionHTTPClient injects a WhaTap-instrumented http.Client so every
	// Slack Web API call appears as an HTTPC step under the parent TX (e.g.
	// /Background-RunAllScans). Without this slack-go falls back to http.DefaultClient
	// and the outbound calls are invisible to WhaTap.
	return &SlackClient{
		api:      slack.New(token, slack.OptionHTTPClient(whataphttpx.Client())),
		users:    make(map[string]slack.User),
		channels: make(map[string]string),
	}
}

func (s *SlackClient) GetAPI() *slack.Client {
	return s.api
}

func (s *SlackClient) LookupChannels() ([]slack.Channel, string, error) {
	//Why: Uses GetConversationsForUser to accurately retrieve only the subset of channels and DM lists where the bot is explicitly invited.
	return s.api.GetConversationsForUser(&slack.GetConversationsForUserParameters{
		Types:           []string{"public_channel", "private_channel", "im", "mpim"},
		ExcludeArchived: true,
		Limit:           1000,
	})
}

func (s *SlackClient) FetchUsers() error {
	users, err := s.api.GetUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		s.users[u.ID] = u
	}
	return nil
}

func (s *SlackClient) LookupUserByEmail(email string) (*slack.User, error) {
	return s.api.GetUserByEmail(email)
}

func (s *SlackClient) GetUserName(id string) string {
	if u, ok := s.users[id]; ok {
		if u.RealName != "" {
			return u.RealName
		}
		return u.Name
	}
	return id
}

func (s *SlackClient) GetChannelName(id string) string {
	if name, ok := s.channels[id]; ok {
		return name
	}
	channel, err := s.api.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID: id,
	})
	if err == nil {
		s.channels[id] = channel.Name
		return channel.Name
	}
	return id
}

// Why: Exported so scanner-side callers (e.g. sweepSlackThreads) can wrap raw API
// calls without re-implementing Retry-After handling. Internal callers retain the
// shorter alias to preserve git blame.
func WithSlackRetry(maxRetries int, contextMsg string, attemptFunc func() error) error {
	return withSlackRetry(maxRetries, contextMsg, attemptFunc)
}

// Why: Implements a retry wrapper that respects Slack's 'Retry-After' header to handle API rate limiting gracefully during heavy scans.
func withSlackRetry(maxRetries int, contextMsg string, attemptFunc func() error) error {
	var err error
	for i := 0; i <= maxRetries; i++ {
		err = attemptFunc()
		if err == nil {
			return nil
		}
		var rateLimitedError *slack.RateLimitedError
		if errors.As(err, &rateLimitedError) {
			logger.Warnf("[SLACK-API] Rate limited on %s. Retrying after %v (attempt %d/%d)", contextMsg, rateLimitedError.RetryAfter, i+1, maxRetries)
			time.Sleep(rateLimitedError.RetryAfter)
			continue
		}
		break
	}
	return err
}

func ParseSlackTimestamp(ts string) time.Time {
	var sec, nsec int64
	fmt.Sscanf(ts, "%d.%d", &sec, &nsec)
	return time.Unix(sec, nsec*1000)
}

func (s *SlackClient) GetMessages(channelID string, since time.Time, lastTS string) ([]types.RawMessage, error) {
	var msgs []types.RawMessage
	cursor := ""
	maxRetries := 3 //Why: Limits API retries to 3 attempts to prevent infinite loops during persistent Slack outages.

	for {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    lastTS,
			Limit:     100,
			Cursor:    cursor,
		}

		var history *slack.GetConversationHistoryResponse
		err := withSlackRetry(maxRetries, fmt.Sprintf("channel %s", channelID), func() error {
			var e error
			history, e = s.api.GetConversationHistory(params)
			return e
		})

		if err != nil {
			return nil, err
		}

		pageMsgs, reachedOld := s.processHistoryMessages(channelID, history.Messages, since)
		msgs = append(msgs, pageMsgs...)

		if reachedOld || !history.HasMore || history.ResponseMetaData.NextCursor == "" {
			break
		}
		cursor = history.ResponseMetaData.NextCursor
	}

	//Why: Optionally reverses message order, though the global scanner typically handles chronological sorting after aggregation.
	return msgs, nil
}

// Why: Separates the message filtering, mapping, and thread expansion logic from the main pagination loop.
func (s *SlackClient) processHistoryMessages(channelID string, messages []slack.Message, since time.Time) ([]types.RawMessage, bool) {
	var msgs []types.RawMessage
	reachedOld := false

	for _, m := range messages {
		ts := ParseSlackTimestamp(m.Timestamp)

		//Why: Terminates the history fetch loop when encountering messages older than the 'since' threshold, as Slack returns results in descending chronological order.
		if ts.Before(since) {
			reachedOld = true
			break
		}

		//Why: Filters out automated bot messages and empty notifications to focus analysis on actionable user-generated task descriptions.
		if m.BotID != "" || m.Text == "" {
			logger.Debugf("[SLACK-DEBUG] Dropping msg: ID=%s, BotID=%s, TextLen=%d", m.Timestamp, m.BotID, len(m.Text))
			continue
		}

		msgs = append(msgs, types.RawMessage{
			ID:              m.Timestamp,
			Sender:          s.GetUserName(m.User),
			SenderName:      s.GetUserName(m.User),
			Text:            m.Text,
			Timestamp:       ts,
			HasAttachment:   len(m.Files) > 0,
			AttachmentNames: s.ExtractFileNames(m.Files),
			Reactions:       s.ExtractReactions(m.Reactions),
			IsPinned:        len(m.PinnedTo) > 0,
		})

		//Why: Recursively fetches thread replies if a message is a thread parent to ensure full conversation context for task extraction.
		if m.ReplyCount > 0 && m.ThreadTimestamp == m.Timestamp {
			replies, err := s.FetchNewThreadReplies(channelID, m.Timestamp, m.Timestamp)
			if err == nil {
				msgs = append(msgs, replies...)
			} else {
				logger.Warnf("[SLACK-API] Failed to fetch thread replies for %s: %v", m.Timestamp, err)
			}
		}
	}
	return msgs, reachedOld
}

// Why: Handles pagination and rate-limiting to fetch all replies in a Slack thread.
func (s *SlackClient) FetchNewThreadReplies(channelID, threadTS, sinceTS string) ([]types.RawMessage, error) {
	var msgs []types.RawMessage
	cursor := ""
	maxRetries := 3

	for {
		params := &slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Cursor:    cursor,
			Oldest:    sinceTS,
		}

		var replies []slack.Message
		var hasMore bool
		var nextCursor string

		err := withSlackRetry(maxRetries, "thread replies", func() error {
			var e error
			replies, hasMore, nextCursor, e = s.api.GetConversationReplies(params)
			return e
		})

		if err != nil {
			return nil, err
		}

		msgs = append(msgs, s.processThreadReplies(threadTS, replies)...)

		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return msgs, nil
}

// Why: Isolates the thread reply filtering and mapping logic from the pagination control flow.
func (s *SlackClient) processThreadReplies(threadTS string, replies []slack.Message) []types.RawMessage {
	var msgs []types.RawMessage
	for _, m := range replies {
		//Why: Skips the parent message within the thread reply list to avoid duplicate processing of the initial task request.
		if m.Timestamp == threadTS {
			continue
		}
		if m.BotID != "" || m.Text == "" {
			continue
		}

		msgs = append(msgs, types.RawMessage{
			ID:              m.Timestamp,
			Sender:          s.GetUserName(m.User),
			SenderName:      s.GetUserName(m.User),
			Text:            m.Text,
			Timestamp:       ParseSlackTimestamp(m.Timestamp),
			ReplyToID:       threadTS, //Why: Attaches thread metadata to extracted replies to maintain relational integrity and correctly group related tasks in the UI.
			HasAttachment:   len(m.Files) > 0,
			AttachmentNames: s.ExtractFileNames(m.Files),
			Reactions:       s.ExtractReactions(m.Reactions),
			IsPinned:        len(m.PinnedTo) > 0,
		})
	}
	return msgs
}
func (s *SlackClient) ExtractFileNames(files []slack.File) []string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		if f.Name != "" {
			names = append(names, f.Name)
		}
	}
	return names
}

func (s *SlackClient) ExtractReactions(reactions []slack.ItemReaction) []string {
	names := make([]string, 0, len(reactions))
	for _, r := range reactions {
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	return names
}
