package services

import (
	"context"
	"errors"
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
	Kind string // "tasks" | "done" | "grant" | "revoke" | "help" | "" (unknown)
	Arg  string // raw arg slice (e.g. task ID for "done")
}

var (
	errUserNotFound  = errors.New("user not found")
	errUserAmbiguous = errors.New("ambiguous user name")
)

// ParseDMCommand extracts the command from a freeform DM/mention body.
// Why: Slack `app_mention` events prefix the bot user id (`<@U123> tasks`); strip it
//
//	so the same parser works for IM (no prefix) and mentions.
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
		if len(fields) >= 2 {
			return SlackDMCommand{Kind: "tasks", Arg: strings.Join(fields[1:], " ")}
		}
		return SlackDMCommand{Kind: "tasks"}
	case "done", "complete", "finish":
		if len(fields) < 2 {
			return SlackDMCommand{Kind: "done"}
		}
		return SlackDMCommand{Kind: "done", Arg: fields[1]}
	case "grant", "allow":
		if len(fields) < 2 {
			return SlackDMCommand{Kind: "grant"}
		}
		return SlackDMCommand{Kind: "grant", Arg: strings.Join(fields[1:], " ")}
	case "revoke", "deny":
		if len(fields) < 2 {
			return SlackDMCommand{Kind: "revoke"}
		}
		return SlackDMCommand{Kind: "revoke", Arg: strings.Join(fields[1:], " ")}
	case "help", "?":
		return SlackDMCommand{Kind: "help"}
	}
	return SlackDMCommand{}
}

// HandleDMText routes a parsed DM/mention to the matching action.
// dmChannel is the IM channel (or app_mention channel) — used as the reply destination.
func (b *SlackBot) HandleDMText(ctx context.Context, slackUserID, dmChannel, text string) error {
	cmd := ParseDMCommand(text)
	user, err := b.resolveUser(ctx, slackUserID)
	if err != nil {
		return err
	}
	switch cmd.Kind {
	case "tasks":
		return b.handleTasksCmd(ctx, user, dmChannel, cmd.Arg)
	case "done":
		id, parseErr := strconv.ParseInt(strings.TrimSpace(cmd.Arg), 10, 64)
		if parseErr != nil || id <= 0 {
			return b.client.SendDM(ctx, dmChannel, "사용법: `done <task id>` (예: `done 1234`)")
		}
		return b.completeTask(ctx, user.Email, dmChannel, store.MessageID(id))
	case "grant":
		return b.handleGrantCmd(ctx, user, dmChannel, cmd.Arg)
	case "revoke":
		return b.handleRevokeCmd(ctx, user, dmChannel, cmd.Arg)
	case "help", "":
		return b.sendHelp(ctx, dmChannel)
	}
	return nil
}

// handleTasksCmd handles the "tasks" command, optionally scoped to another user.
func (b *SlackBot) handleTasksCmd(ctx context.Context, user *store.User, dmChannel, arg string) error {
	if arg == "" {
		return b.HandleListTasks(ctx, user.Email, dmChannel, 0)
	}
	target, candidates, err := b.resolveUserByName(ctx, arg)
	if errors.Is(err, errUserNotFound) {
		return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("사용자를 찾을 수 없습니다: `%s`", arg))
	}
	if errors.Is(err, errUserAmbiguous) {
		return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("여러 사용자가 일치합니다: %s", candidates))
	}
	if err != nil {
		return err
	}
	if target.ID == user.ID {
		return b.HandleListTasks(ctx, user.Email, dmChannel, 0)
	}
	granted, err := store.IsGrantedToView(ctx, user.ID, target.ID)
	if err != nil {
		return fmt.Errorf("check grant for %s→%s: %w", user.Email, target.Email, err)
	}
	if !granted {
		return b.client.SendDM(ctx, dmChannel, "권한이 없습니다")
	}
	return b.HandleListTasksFor(ctx, target, dmChannel, 0)
}

// handleGrantCmd grants the caller's task view permission to a named user.
func (b *SlackBot) handleGrantCmd(ctx context.Context, user *store.User, dmChannel, arg string) error {
	if arg == "" {
		return b.client.SendDM(ctx, dmChannel, "사용법: `grant <이름>`")
	}
	target, candidates, err := b.resolveUserByName(ctx, arg)
	if errors.Is(err, errUserNotFound) {
		return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("사용자를 찾을 수 없습니다: `%s`", arg))
	}
	if errors.Is(err, errUserAmbiguous) {
		return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("여러 사용자가 일치합니다: %s", candidates))
	}
	if err != nil {
		return err
	}
	if target.ID == user.ID {
		return b.client.SendDM(ctx, dmChannel, "자기 자신에게는 권한을 부여할 수 없습니다")
	}
	if err := store.CreateGrant(ctx, user.ID, target.ID); err != nil {
		return fmt.Errorf("create grant %s→%s: %w", user.Email, target.Email, err)
	}
	return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("`%s`에게 내 task 조회 권한을 부여했습니다 :white_check_mark:", userDisplayName(target)))
}

// handleRevokeCmd revokes a previously granted task view permission.
func (b *SlackBot) handleRevokeCmd(ctx context.Context, user *store.User, dmChannel, arg string) error {
	if arg == "" {
		return b.client.SendDM(ctx, dmChannel, "사용법: `revoke <이름>`")
	}
	target, candidates, err := b.resolveUserByName(ctx, arg)
	if errors.Is(err, errUserNotFound) {
		return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("사용자를 찾을 수 없습니다: `%s`", arg))
	}
	if errors.Is(err, errUserAmbiguous) {
		return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("여러 사용자가 일치합니다: %s", candidates))
	}
	if err != nil {
		return err
	}
	if err := store.RevokeGrant(ctx, user.ID, target.ID); err != nil {
		return fmt.Errorf("revoke grant %s→%s: %w", user.Email, target.Email, err)
	}
	return b.client.SendDM(ctx, dmChannel, fmt.Sprintf("`%s`의 task 조회 권한을 회수했습니다", userDisplayName(target)))
}

// HandleListTasks fetches the user's active tasks and posts a fresh Block Kit list to channel.
func (b *SlackBot) HandleListTasks(ctx context.Context, email, channel string, page int) error {
	tasks, total, err := b.fetchActiveTasks(ctx, email, page, SlackBotPageSize)
	if err != nil {
		return err
	}
	blocks, fallback := BuildTaskListBlocks(tasks, page, SlackBotPageSize, total, "")
	return b.client.SendDMBlocks(ctx, channel, blocks, fallback)
}

// HandleListTasksFor fetches tasks for a delegated user and posts them to channel.
func (b *SlackBot) HandleListTasksFor(ctx context.Context, target *store.User, channel string, page int) error {
	tasks, total, err := b.fetchActiveTasks(ctx, target.Email, page, SlackBotPageSize)
	if err != nil {
		return err
	}
	blocks, fallback := BuildTaskListBlocks(tasks, page, SlackBotPageSize, total, target.Name)
	return b.client.SendDMBlocks(ctx, channel, blocks, fallback)
}

// HandleDoneAction marks the task done and rewrites the Block Kit message in place.
// Why: Called from the interactive (block_actions) handler — channel/ts come from the
//
//	payload's container, so the user sees the same message refresh instead of a new DM.
func (b *SlackBot) HandleDoneAction(ctx context.Context, slackUserID, channel, messageTS string, taskID store.MessageID, page int) error {
	user, err := b.resolveUser(ctx, slackUserID)
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
	blocks, fallback := BuildTaskListBlocks(tasks, page, SlackBotPageSize, total, "")
	return b.client.UpdateDMBlocks(ctx, channel, messageTS, blocks, fallback)
}

// HandlePageAction re-renders the list message at a different page.
func (b *SlackBot) HandlePageAction(ctx context.Context, slackUserID, channel, messageTS string, page int) error {
	user, err := b.resolveUser(ctx, slackUserID)
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
	blocks, fallback := BuildTaskListBlocks(tasks, page, SlackBotPageSize, total, "")
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
		"• `tasks` — 내 활성 task 목록 보기",
		"• `tasks <이름>` — 권한 받은 사용자의 task 목록",
		"• `done <id>` — task 완료 처리 (예: `done 1234`)",
		"• `grant <이름>` — 그 사람이 내 task를 볼 수 있도록 허가",
		"• `revoke <이름>` — 권한 회수",
		"• `help` — 이 도움말",
		"",
		"_또는 목록의 `완료` 버튼을 눌러도 됩니다._",
	}, "\n")
	return b.client.SendDM(ctx, channel, msg)
}

// resolveUser maps slackUserID → User (via stored slack_id mapping). On miss, sends a guidance DM.
// Why: target the user's IM (slackUserID), not the caller-supplied channel — app_mention may
// pass a public channel where leaking "이 Slack 계정이…" exposes company context to bystanders.
func (b *SlackBot) resolveUser(ctx context.Context, slackUserID string) (*store.User, error) {
	user, err := store.GetUserBySlackID(ctx, slackUserID)
	if err != nil || user == nil {
		_ = b.client.SendDM(ctx, slackUserID, "이 Slack 계정이 message-consolidator 사용자와 연결되어 있지 않습니다.\nGoogle 로그인 시 같은 회사 이메일로 접속하면 자동 연결됩니다.")
		return nil, fmt.Errorf("slack id %s not mapped to user: %w", slackUserID, err)
	}
	return user, nil
}

// resolveUserByName looks up a user by display name or alias (case-insensitive).
// Returns (user, "", nil) on unique match, (nil, candidates, errUserAmbiguous) on multiple,
// (nil, "", errUserNotFound) on no match.
// stripSlackAutoLink unwraps Slack's auto-formatted links.
// Why: Slack rich-text converts emails to <mailto:addr|addr> and URLs to <url|text>
// before delivering the event payload, so raw string comparison fails without stripping.
func userDisplayName(u *store.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func stripSlackAutoLink(s string) string {
	if !strings.HasPrefix(s, "<") || !strings.HasSuffix(s, ">") {
		return s
	}
	inner := s[1 : len(s)-1]
	if strings.HasPrefix(inner, "mailto:") {
		addr := strings.TrimPrefix(inner, "mailto:")
		if i := strings.Index(addr, "|"); i >= 0 {
			addr = addr[:i]
		}
		return addr
	}
	if i := strings.Index(inner, "|"); i >= 0 {
		return inner[i+1:]
	}
	return inner
}

func (b *SlackBot) resolveUserByName(ctx context.Context, name string) (*store.User, string, error) {
	all, err := store.GetAllUsers(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("load users: %w", err)
	}
	lower := strings.ToLower(stripSlackAutoLink(name))
	var matches []store.User
	for _, u := range all {
		if strings.ToLower(u.Email) == lower || (u.Name != "" && strings.ToLower(u.Name) == lower) {
			matches = append(matches, u)
			continue
		}
		for _, alias := range u.Aliases {
			if strings.ToLower(alias) == lower {
				matches = append(matches, u)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, "", errUserNotFound
	case 1:
		u := matches[0]
		return &u, "", nil
	default:
		names := make([]string, len(matches))
		for i, u := range matches {
			names[i] = u.Name
		}
		return nil, strings.Join(names, ", "), errUserAmbiguous
	}
}

// fetchActiveTasks returns the page slice + total active count (done=0, lifecycle=active).
// Why: store.GetMessages already serves the cached active-only list for the user, so
//
//	paging in Go avoids a new SQL query for low-cardinality DM use.
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
