package channels

import (
	"errors"
	"fmt"
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
	return &SlackClient{
		api:      slack.New(token),
		users:    make(map[string]slack.User),
		channels: make(map[string]string),
	}
}

func (s *SlackClient) LookupChannels() ([]slack.Channel, string, error) {
	// GetConversationsForUser를 사용하면 봇(Bot)이 초대된 채널과 DM 목록만 정확히 반환합니다.
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

func (s *SlackClient) GetMessages(channelID string, since time.Time, lastTS string) ([]types.RawMessage, error) {
	var msgs []types.RawMessage
	cursor := ""
	maxRetries := 3 // 최대 3번까지 재시도 허용

	for {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    lastTS,
			Limit:     100,
			Cursor:    cursor,
		}

		var history *slack.GetConversationHistoryResponse
		var err error

		for i := 0; i <= maxRetries; i++ {
			history, err = s.api.GetConversationHistory(params)
			if err == nil {
				break // 정상 응답 시 루프 탈출
			}

			// Slack Rate Limit(HTTP 429) 에러인 경우 감지
			var rateLimitedError *slack.RateLimitedError
			if errors.As(err, &rateLimitedError) {
				logger.Warnf("[SLACK-API] Rate limited on channel %s. Retrying after %v (attempt %d/%d)", channelID, rateLimitedError.RetryAfter, i+1, maxRetries)
				time.Sleep(rateLimitedError.RetryAfter) // Slack이 요구한 시간만큼 정확히 대기 후 재시도
				continue
			}
			break // Rate Limit 이외의 에러(토큰 만료 등)는 즉시 중단
		}

		if err != nil {
			return nil, err
		}

		for _, m := range history.Messages {
			// Skip bot messages or messages without text
			if m.BotID != "" || m.Text == "" {
				continue
			}

			// Slack TS is something like 1612345678.000100
			var sec, nsec int64
			fmt.Sscanf(m.Timestamp, "%d.%d", &sec, &nsec)
			ts := time.Unix(sec, nsec*1000)

			if ts.Before(since) {
				continue
			}

			msgs = append(msgs, types.RawMessage{
				ID:        m.Timestamp,
				Sender:    s.GetUserName(m.User),
				Text:      m.Text,
				Timestamp: ts,
			})

			// 스레드(Thread)에 답글이 있는 경우 추가 수집
			if m.ReplyCount > 0 && m.ThreadTimestamp == m.Timestamp {
				replies, err := s.FetchNewThreadReplies(channelID, m.Timestamp, m.Timestamp)
				if err == nil {
					msgs = append(msgs, replies...)
				} else {
					logger.Warnf("[SLACK-API] Failed to fetch thread replies for %s: %v", m.Timestamp, err)
				}
			}
		}

		if !history.HasMore || history.ResponseMetaData.NextCursor == "" {
			break
		}
		cursor = history.ResponseMetaData.NextCursor
	}

	// Reverse to get chronological order if needed, but scanner handles it
	return msgs, nil
}

// FetchNewThreadReplies handles pagination and rate-limiting to fetch all replies in a Slack thread.
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
		var err error

		for i := 0; i <= maxRetries; i++ {
			replies, hasMore, nextCursor, err = s.api.GetConversationReplies(params)
			if err == nil {
				break
			}

			var rateLimitedError *slack.RateLimitedError
			if errors.As(err, &rateLimitedError) {
				logger.Warnf("[SLACK-API] Rate limited on thread replies. Retrying after %v (attempt %d/%d)", rateLimitedError.RetryAfter, i+1, maxRetries)
				time.Sleep(rateLimitedError.RetryAfter)
				continue
			}
			break
		}

		if err != nil {
			return nil, err
		}

		for _, m := range replies {
			// 부모 메시지 원본은 이미 history에서 수집했으므로 중복 스킵
			if m.Timestamp == threadTS {
				continue
			}
			if m.BotID != "" || m.Text == "" {
				continue
			}

			var sec, nsec int64
			fmt.Sscanf(m.Timestamp, "%d.%d", &sec, &nsec)
			ts := time.Unix(sec, nsec*1000)

			msgs = append(msgs, types.RawMessage{
				ID:        m.Timestamp,
				Sender:    s.GetUserName(m.User),
				Text:      m.Text,
				Timestamp: ts,
				ReplyToID: threadTS, // 스레드 소속 메타데이터 부여!
			})
		}

		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return msgs, nil
}
