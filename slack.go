package main

import (
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
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

func (s *SlackClient) GetMessages(channelID string, since time.Time, lastTS string) ([]store.RawChatMessage, error) {
	var allMsgs []store.RawChatMessage

	// Determine the effective starting point
	oldest := fmt.Sprintf("%d.%06d", since.Unix(), 0)
	if lastTS != "" {
		oldest = lastTS
	}

	cursor := ""
	for {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    oldest,
			Cursor:    cursor,
			Limit:     50, // Smaller batches for efficiency
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

			// Add thread replies if any and if thread has NEW updates after our last scan
			// Note: Slack's LatestReply is a timestamp string
			if m.ReplyCount > 0 && (lastTS == "" || m.LatestReply > lastTS) {
				allMsgs = append(allMsgs, s.fetchThreadReplies(channelID, m.Timestamp, oldest)...)
			}
		}

		if !history.HasMore {
			break
		}
		cursor = history.ResponseMetadata.Cursor
	}

	return allMsgs, nil
}

// 별도로 분리된 스레드 답글 수집 헬퍼 함수
func (s *SlackClient) fetchThreadReplies(channelID, threadTS, oldest string) []store.RawChatMessage {
	var threadMsgs []store.RawChatMessage

	replies, _, _, err := s.api.GetConversationReplies(&slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Oldest:    oldest, // Only fetch new replies
	})
	if err != nil {
		logger.Debugf("[SCAN-SLACK] Failed to fetch replies for thread %s in channel %s: %v", threadTS, channelID, err)
		return threadMsgs
	}

	for _, r := range replies {
		// Skip the parent as it's already added in the main conversation history
		if r.Timestamp != threadTS {
			threadMsgs = append(threadMsgs, s.parseMessage(r))
		}
	}
	return threadMsgs
}

func (s *SlackClient) parseMessage(m slack.Message) store.RawChatMessage {
	parts := strings.Split(m.Timestamp, ".")
	var ts time.Time
	if len(parts) > 0 {
		sec, _ := strconv.ParseInt(parts[0], 10, 64)
		ts = time.Unix(sec, 0)
	}
	return store.RawChatMessage{
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
