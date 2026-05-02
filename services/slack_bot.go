package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

// SlackDMer is the consumer-defined surface SlackBot needs from a Slack client.
// Why: avoids services→channels import cycle (channels/gmail.go already imports services).
// *channels.SlackClient satisfies this implicitly.
type SlackDMer interface {
	SendDM(ctx context.Context, channel, text string) error
	SendDMBlocks(ctx context.Context, channel string, blocks []slack.Block, fallback string) error
	UpdateDMBlocks(ctx context.Context, channel, ts string, blocks []slack.Block, fallback string) error
}

// SlackBot dispatches DM commands and Block Kit interactions for the Slack-side task UI.
// Why: Service layer keeps protocol details (HTTP/payload parsing) in handlers; this type
// only deals with parsed (slackUserID, command, args) tuples so it stays unit-testable.
type SlackBot struct {
	client SlackDMer
	tasks  *TasksService
}

func NewSlackBot(client SlackDMer, tasks *TasksService) *SlackBot {
	return &SlackBot{client: client, tasks: tasks}
}

// SlackDMCommand is the parsed result of a DM/mention text payload.
type SlackDMCommand struct {
	Kind string // "tasks" | "done" | "help" | "" (unknown)
	Arg  string // raw arg slice (e.g. task ID for "done")
}

// ParseDMCommand extracts the command from a freeform DM/mention body.
// Why: Slack `app_mention` events prefix the bot user id (`<@U123> tasks`); strip it
//      so the same parser works for IM (no prefix) and mentions.
func ParseDMCommand(text string) SlackDMCommand {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "<@") {
		if end := strings.Index(t, ">"); end > 0 {
			t = strings.TrimSpace(t[end+1:])
		}
	}
	if t == "" {
		return SlackDMCommand{Kind: "help"}
	}
	fields := strings.Fields(t)
	head := strings.ToLower(fields[0])
	switch head {
	case "tasks", "task", "list", "ls":
		return SlackDMCommand{Kind: "tasks"}
	case "done", "complete", "finish":
		if len(fields) < 2 {
			return SlackDMCommand{Kind: "done"}
		}
		return SlackDMCommand{Kind: "done", Arg: fields[1]}
	case "help", "?":
		return SlackDMCommand{Kind: "help"}
	}
	return SlackDMCommand{}
}

// HandleDMText routes a parsed DM/mention to the matching action.
// dmChannel is the IM channel (or app_mention channel) — used as the reply destination.
func (b *SlackBot) HandleDMText(ctx context.Context, slackUserID, dmChannel, text string) error {
	cmd := ParseDMCommand(text)
	user, err := b.resolveUser(ctx, slackUserID, dmChannel)
	if err != nil {
		return err
	}
	switch cmd.Kind {
	case "tasks":
		return b.HandleListTasks(ctx, user.Email, dmChannel, 0)
	case "done":
		id, parseErr := strconv.ParseInt(strings.TrimSpace(cmd.Arg), 10, 64)
		if parseErr != nil || id <= 0 {
			return b.client.SendDM(ctx, dmChannel, "사용법: `done <task id>` (예: `done 1234`)")
		}
		return b.completeTask(ctx, user.Email, dmChannel, store.MessageID(id))
	case "help", "":
		return b.sendHelp(ctx, dmChannel)
	}
	return nil
}

// HandleListTasks fetches the user's active tasks and posts a fresh Block Kit list to channel.
func (b *SlackBot) HandleListTasks(ctx context.Context, email, channel string, page int) error {
	tasks, total, err := b.fetchActiveTasks(ctx, email, page, SlackBotPageSize)
	if err != nil {
		return err
	}
	blocks, fallback := BuildTaskListBlocks(tasks, page, SlackBotPageSize, total)
	return b.client.SendDMBlocks(ctx, channel, blocks, fallback)
}

// HandleDoneAction marks the task done and rewrites the Block Kit message in place.
// Why: Called from the interactive (block_actions) handler — channel/ts come from the
//      payload's container, so the user sees the same message refresh instead of a new DM.
func (b *SlackBot) HandleDoneAction(ctx context.Context, slackUserID, channel, messageTS string, taskID store.MessageID, page int) error {
	user, err := b.resolveUser(ctx, slackUserID, channel)
	if err != nil {
		return err
	}
	if err := b.tasks.HandleTaskCompletion(ctx, user.Email, taskID, true); err != nil {
		return fmt.Errorf("complete task %d: %w", taskID, err)
	}
	tasks, total, err := b.fetchActiveTasks(ctx, user.Email, page, SlackBotPageSize)
	if err != nil {
		return err
	}
	blocks, fallback := BuildTaskListBlocks(tasks, page, SlackBotPageSize, total)
	return b.client.UpdateDMBlocks(ctx, channel, messageTS, blocks, fallback)
}

// HandlePageAction re-renders the list message at a different page.
func (b *SlackBot) HandlePageAction(ctx context.Context, slackUserID, channel, messageTS string, page int) error {
	user, err := b.resolveUser(ctx, slackUserID, channel)
	if err != nil {
		return err
	}
	if page < 0 {
		page = 0
	}
	tasks, total, err := b.fetchActiveTasks(ctx, user.Email, page, SlackBotPageSize)
	if err != nil {
		return err
	}
	blocks, fallback := BuildTaskListBlocks(tasks, page, SlackBotPageSize, total)
	return b.client.UpdateDMBlocks(ctx, channel, messageTS, blocks, fallback)
}

// completeTask is the text-DM path: mark done, then post a short text ack.
func (b *SlackBot) completeTask(ctx context.Context, email, channel string, id store.MessageID) error {
	if err := b.tasks.HandleTaskCompletion(ctx, email, id, true); err != nil {
		logger.Warnf("[SLACKBOT] complete task %d for %s failed: %v", id, email, err)
		return b.client.SendDM(ctx, channel, fmt.Sprintf(":warning: task %d 완료 처리 실패", id))
	}
	return b.client.SendDM(ctx, channel, fmt.Sprintf(":white_check_mark: task %d 완료 처리 완료", id))
}

func (b *SlackBot) sendHelp(ctx context.Context, channel string) error {
	msg := strings.Join([]string{
		"*사용 가능한 명령*",
		"• `tasks` — 활성 task 목록 보기",
		"• `done <id>` — task 완료 처리 (예: `done 1234`)",
		"• `help` — 이 도움말",
		"",
		"_또는 목록의 `완료` 버튼을 눌러도 됩니다._",
	}, "\n")
	return b.client.SendDM(ctx, channel, msg)
}

// resolveUser maps slackUserID → User (via stored slack_id mapping). On miss, sends a guidance DM.
func (b *SlackBot) resolveUser(ctx context.Context, slackUserID, channel string) (*store.User, error) {
	user, err := store.GetUserBySlackID(ctx, slackUserID)
	if err != nil || user == nil {
		_ = b.client.SendDM(ctx, channel, "이 Slack 계정이 message-consolidator 사용자와 연결되어 있지 않습니다.\nGoogle 로그인 시 같은 회사 이메일로 접속하면 자동 연결됩니다.")
		return nil, fmt.Errorf("slack id %s not mapped to user: %w", slackUserID, err)
	}
	return user, nil
}

// fetchActiveTasks returns the page slice + total active count (done=0, lifecycle=active).
// Why: store.GetMessages already serves the cached active-only list for the user, so
//      paging in Go avoids a new SQL query for low-cardinality DM use.
func (b *SlackBot) fetchActiveTasks(ctx context.Context, email string, page, pageSize int) ([]store.ConsolidatedMessage, int, error) {
	all, err := store.GetMessages(ctx, email)
	if err != nil {
		return nil, 0, fmt.Errorf("load active tasks for %s: %w", email, err)
	}
	active := make([]store.ConsolidatedMessage, 0, len(all))
	for _, m := range all {
		if !m.Done {
			active = append(active, m)
		}
	}
	total := len(active)
	start := page * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return active[start:end], total, nil
}
