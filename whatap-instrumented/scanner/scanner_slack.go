package scanner

import (
	"context"
	"fmt"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strconv"
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
				if classification == "내 업무" || classification == "회신 대기" {
					link := fmt.Sprintf("https://slack.com/archives/%s/p%s", c.ID, strings.ReplaceAll(m.ID, ".", ""))
					if m.ReplyToID != "" {
						// 스레드 발견 시 스위퍼 감시 목록에 등록 (최초 등록만 수행)
						_ = store.RegisterActiveSlackThread(user.Email, c.ID, m.ReplyToID)

						link += fmt.Sprintf("?thread_ts=%s", m.ReplyToID) // 클릭 시 우측 스레드 창이 즉시 열리도록 링크 강화
					}

					assigneeName := user.Name
					if assigneeName == "" {
						assigneeName = user.Email
					}

					taskText := m.Text
					category := "todo"

					if classification == "회신 대기" {
						category = "waiting"
						// assigneeName을 임의의 '수신자'로 덮어쓰지 않고 원래 이름을 유지하거나 공백 처리
					}

					msgsToSave = append(msgsToSave, store.ConsolidatedMessage{
						UserEmail:    user.Email,
						Source:       "slack",
						Room:         sc.GetChannelName(c.ID),
						Task:         taskText,
						Requester:    m.Sender,
						Assignee:     assigneeName,
						AssignedAt:   m.Timestamp,
						Link:         link,
						SourceTS:     m.ID,
						OriginalText: m.Text,
						Category:     category,
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
	// 1. 발신자가 나인지 확인
	isFromMe := strings.EqualFold(m.Sender, user.Name) || strings.EqualFold(m.Sender, user.Email)

	// 내가 보낸 메시지인데, 다른 사람을 멘션(@U...)했다면 회신 대기로 판단
	if isFromMe && strings.Contains(m.Text, "<@U") && (user.SlackID == "" || !strings.Contains(m.Text, "<@"+user.SlackID+">")) {
		return "회신 대기"
	}

	// 2. DM이거나 내가 멘션된 경우 내 업무 판단
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

func startSlowSweeper() {
	logger.Infof("Slack Slow Sweeper started (monitoring old threads)...")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sweepSlackThreads()
	}
}

func sweepSlackThreads() {
	if cfg == nil || cfg.SlackToken == "" {
		return
	}

	threads, err := store.GetActiveSlackThreads()
	if err != nil || len(threads) == 0 {
		return
	}

	sc := channels.NewSlackClient(cfg.SlackToken)
	if err := sc.FetchUsers(); err != nil {
		logger.Warnf("[SCAN-SWEEPER] Failed to fetch users: %v", err)
	}

	for _, t := range threads {
		// 14일 이상 경과한 오래된 스레드는 모니터링 종료 (자동 정리)
		tsParts := strings.Split(t.ThreadTS, ".")
		if len(tsParts) > 0 {
			if sec, err := strconv.ParseInt(tsParts[0], 10, 64); err == nil {
				if time.Since(time.Unix(sec, 0)) > 14*24*time.Hour {
					_ = store.RemoveActiveSlackThread(t.UserEmail, t.ChannelID, t.ThreadTS)
					continue
				}
			}
		}

		user, err := store.GetOrCreateUser(t.UserEmail, "", "")
		if err != nil {
			continue
		}
		aliases, _ := store.GetUserAliases(user.ID)
		effectiveAliases := getEffectiveAliases(*user, aliases)

		replies, err := sc.FetchNewThreadReplies(t.ChannelID, t.ThreadTS, t.LastTS)
		if err != nil || len(replies) == 0 {
			time.Sleep(3 * time.Second) // Rate Limit 방어를 위한 지연
			continue
		}

		var msgsToSave []store.ConsolidatedMessage
		newLastTS := t.LastTS

		for _, m := range replies {
			// 스위퍼에서는 채널의 메타데이터 전체가 필요하지 않으므로 ID만 임시 부여하여 분류합니다.
			classification := classifyMessage(slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: t.ChannelID}}}, user, effectiveAliases, m)
			if classification == "내 업무" || classification == "회신 대기" {
				assigneeName := user.Name
				if assigneeName == "" {
					assigneeName = user.Email
				}
				taskText := m.Text
				category := "todo"
				if classification == "회신 대기" {
					category = "waiting"
				}
				link := fmt.Sprintf("https://slack.com/archives/%s/p%s?thread_ts=%s", t.ChannelID, strings.ReplaceAll(m.ID, ".", ""), t.ThreadTS)
				msgsToSave = append(msgsToSave, store.ConsolidatedMessage{UserEmail: user.Email, Source: "slack", Room: sc.GetChannelName(t.ChannelID), Task: taskText, Requester: m.Sender, Assignee: assigneeName, AssignedAt: m.Timestamp, Link: link, SourceTS: m.ID, OriginalText: m.Text, Category: category})
			}
			if m.ID > newLastTS {
				newLastTS = m.ID
			}
		}
		if len(msgsToSave) > 0 {
			store.SaveMessages(msgsToSave)
		}
		if newLastTS != t.LastTS {
			_ = store.UpdateSlackThreadLastTS(t.UserEmail, t.ChannelID, t.ThreadTS, newLastTS)
		}
		time.Sleep(3 * time.Second) // API 호출 후 무조건 3초 대기 (분당 20회 제한 준수)
	}
}
