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
	var allMsgs []RawChatMessage
	cursor := ""
	
	for {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    fmt.Sprintf("%d", since.Unix()),
			Cursor:    cursor,
			Limit:     100, // Fetch in batches
		}
		
		history, err := s.api.GetConversationHistory(params)
		if err != nil {
			return nil, err
		}

		for _, m := range history.Messages {
			if m.User == "" || m.Text == "" {
				continue
			}
			
			// Add main message
			allMsgs = append(allMsgs, s.parseMessage(m))

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
							allMsgs = append(allMsgs, s.parseMessage(r))
						}
					}
				}
			}
		}

		if !history.HasMore {
			break
		}
		cursor = history.ResponseMetadata.Cursor
	}
	
	return allMsgs, nil
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

func (s *SlackClient) LookupUserByEmail(email string) (*slack.User, error) {
	user, err := s.api.GetUserByEmail(email)
	if err != nil {
		return nil, err
	}
	return user, nil
}
