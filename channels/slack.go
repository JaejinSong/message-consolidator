package channels

import (
	"fmt"
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
	return s.api.GetConversations(&slack.GetConversationsParameters{
		Types: []string{"public_channel", "private_channel", "im", "mpim"},
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
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Oldest:    lastTS,
		Limit:     100,
	}

	history, err := s.api.GetConversationHistory(params)
	if err != nil {
		return nil, err
	}

	var msgs []types.RawMessage
	for _, m := range history.Messages {
		// Skip bot messages or messages without text
		if m.BotID != "" || m.Text == "" {
			continue
		}

		ts, _ := time.Parse(time.RFC3339, m.Timestamp)
		// ts parsing might fail for slack ts format, using fallback
		if ts.IsZero() {
			// Slack TS is something like 1612345678.000100
			var sec, nsec int64
			fmt.Sscanf(m.Timestamp, "%d.%d", &sec, &nsec)
			ts = time.Unix(sec, nsec*1000)
		}

		if ts.Before(since) {
			continue
		}

		msgs = append(msgs, types.RawMessage{
			ID:        m.Timestamp,
			Sender:    s.GetUserName(m.User),
			Text:      m.Text,
			Timestamp: ts,
		})
	}

	// Reverse to get chronological order if needed, but scanner handles it
	return msgs, nil
}
