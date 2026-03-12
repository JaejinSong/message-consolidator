package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

type SlackClient struct {
	api     *slack.Client
	userMap map[string]string
}

type RawChatMessage struct {
	User           string
	InteractedUser string // 답장 대상 또는 멘션된 사용자
	Text           string
	Timestamp      time.Time
	RawTS          string
}

func NewSlackClient(token string) *SlackClient {
	return &SlackClient{
		api:     slack.New(token),
		userMap: make(map[string]string),
	}
}

func (s *SlackClient) FetchUsers() error {
	users, err := s.api.GetUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		name := u.RealName
		if name == "" {
			name = u.Name
		}
		s.userMap[u.ID] = name
	}
	return nil
}

func (s *SlackClient) GetUserName(id string) string {
	if name, ok := s.userMap[id]; ok {
		return name
	}
	return id
}

func (s *SlackClient) GetMessages(channelID string, since time.Time) ([]RawChatMessage, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Oldest:    fmt.Sprintf("%d", since.Unix()),
	}
	history, err := s.api.GetConversationHistory(params)
	if err != nil {
		return nil, err
	}

	var msgs []RawChatMessage
	for _, m := range history.Messages {
		if m.User == "" || m.Text == "" {
			continue
		}
		
		// Add main message
		msgs = append(msgs, s.parseMessage(m))

		// Add thread replies if any
		if m.ReplyCount > 0 {
			replies, _, _, err := s.api.GetConversationReplies(&slack.GetConversationRepliesParameters{
				ChannelID: channelID,
				Timestamp: m.Timestamp,
			})
			if err == nil {
				for _, r := range replies {
					// Skip the parent as it's already added
					if r.Timestamp != m.Timestamp {
						msgs = append(msgs, s.parseMessage(r))
					}
				}
			}
		}
	}
	return msgs, nil
}

func (s *SlackClient) parseMessage(m slack.Message) RawChatMessage {
	parts := strings.Split(m.Timestamp, ".")
	var ts time.Time
	if len(parts) > 0 {
		sec, _ := strconv.ParseInt(parts[0], 10, 64)
		ts = time.Unix(sec, 0)
	}
	return RawChatMessage{
		User:      s.GetUserName(m.User),
		Text:      m.Text,
		Timestamp: ts,
		RawTS:     m.Timestamp,
	}
}
func (s *SlackClient) GetChannelName(channelID string) string {
	channel, err := s.api.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID:         channelID,
		IncludeLocale:     false,
		IncludeNumMembers: false,
	})
	if err != nil {
		return channelID
	}
	return channel.Name
}
