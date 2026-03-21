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

	"github.com/slack-go/slack"
	"golang.org/x/sync/errgroup"
)

func scanSlack(ctx context.Context, user store.User, aliases []string) {
	if cfg.SlackToken == "" {
		return
	}
	sc := channels.NewSlackClient(cfg.SlackToken)
	if err := sc.FetchUsers(); err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch users: %v", err)
	}

	// Use the Slack API to get channels the bot is a member of
	chans, _, err := sc.LookupChannels()
	if err != nil {
		logger.Errorf("[SCAN-SLACK] Failed to fetch channels: %v", err)
		return
	}

	var eg errgroup.Group
	for _, channel := range chans {
		c := channel
		eg.Go(func() error {
			// 시작 전이나 처리 도중 타임아웃이 발생했는지 감지
			if err := ctx.Err(); err != nil {
				return err
			}

			lastTS := store.GetLastScan(user.Email, "slack", c.ID)
			msgs, err := sc.GetMessages(c.ID, time.Now().Add(-24*time.Hour), lastTS)
			if err != nil {
				logger.Warnf("[SCAN-SLACK] Failed to fetch messages for channel %s (user: %s): %v", c.Name, user.Email, err)
				return err
			}

			if len(msgs) == 0 {
				return nil
			}

			var msgsToSave []store.ConsolidatedMessage
			newLastTS := lastTS
			for _, m := range msgs {
				classification := classifyMessage(c, &user, aliases, m)
				if classification == "내 업무" {
					link := fmt.Sprintf("https://slack.com/archives/%s/p%s", c.ID, strings.ReplaceAll(m.ID, ".", ""))

					assigneeName := user.Name
					if assigneeName == "" {
						assigneeName = user.Email
					}

					msgsToSave = append(msgsToSave, store.ConsolidatedMessage{
						UserEmail:    user.Email,
						Source:       "slack",
						Room:         sc.GetChannelName(c.ID),
						Task:         m.Text,
						Requester:    m.Sender,
						Assignee:     assigneeName,
						AssignedAt:   m.Timestamp.Format(time.RFC3339),
						Link:         link,
						SourceTS:     m.ID,
						OriginalText: m.Text,
					})
				}
				if m.ID > newLastTS {
					newLastTS = m.ID
				}
			}
			if len(msgsToSave) > 0 {
				store.SaveMessages(msgsToSave)
			}
			store.UpdateLastScan(user.Email, "slack", c.ID, newLastTS)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		logger.Debugf("[SCAN-SLACK] Partially completed with errors for %s: %v", user.Email, err)
	}
}

func classifyMessage(channel slack.Channel, user *store.User, aliases []string, m types.RawMessage) string {
	// 1. DM이거나 직접 멘션된 경우 즉시 반환 (Early Return)
	if channel.IsIM || channel.IsMpIM {
		return "내 업무"
	}
	if user.SlackID != "" && strings.Contains(m.Text, "<@"+user.SlackID+">") {
		return "내 업무"
	}

	for _, alias := range aliases {
		if alias != "" && IsAliasMatched(m.Text, m.Sender, alias) {
			return "내 업무"
		}
	}

	return "기타 업무"
}
