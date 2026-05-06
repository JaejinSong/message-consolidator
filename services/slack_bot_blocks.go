package services

import (
	"fmt"
	"message-consolidator/store"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

// SlackBotPageSize caps tasks per Block Kit message; keeps payload below Slack's 50-block hard limit
// (header + N*(section + actions) ≈ 1 + 2N → 24 fits comfortably with footer).
const SlackBotPageSize = 20

// Why: action_id encodes (kind, payload) so the interactive handler can dispatch without
// looking up sidecar state. Stable string keys instead of JSON to keep payload small and
// debuggable in Slack's webhook trace.
const (
	SlackActionTaskDone = "task_done" // action_id format: task_done:<MessageID>
	SlackActionTaskPage = "task_page" // action_id format: task_page:<page>
)

// BuildTaskListBlocks renders a paginated Block Kit list of active tasks for DM display.
// Why: Pure function (no DB / no SDK calls) so snapshot tests can pin the JSON shape.
// `total` is the unpaged count — needed to decide whether the footer page button shows.
// `ownerName` non-empty signals a delegated view: header shows the owner and done buttons are hidden.
func BuildTaskListBlocks(tasks []store.ConsolidatedMessage, page, pageSize, total int, ownerName string) ([]slack.Block, string) {
	if pageSize <= 0 {
		pageSize = SlackBotPageSize
	}
	blocks := make([]slack.Block, 0, len(tasks)*2+2)

	if len(tasks) == 0 {
		emptyText := ":white_check_mark: 활성 task가 없습니다."
		if ownerName != "" {
			emptyText = fmt.Sprintf(":white_check_mark: %s의 활성 task가 없습니다.", ownerName)
		}
		return []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, emptyText, false, false),
				nil, nil,
			),
		}, emptyText
	}

	var header string
	if ownerName != "" {
		header = fmt.Sprintf("*%s의 활성 task %d건* (page %d)", ownerName, total, page+1)
	} else {
		header = fmt.Sprintf("*활성 task %d건* (page %d)", total, page+1)
	}
	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, header, false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),
	)

	for _, t := range tasks {
		if ownerName != "" {
			blocks = append(blocks, taskRowBlockReadOnly(t))
		} else {
			blocks = append(blocks, taskRowBlock(t))
		}
	}

	if hasNextPage(page, pageSize, total) {
		nextPage := page + 1
		btn := slack.NewButtonBlockElement(
			SlackActionTaskPage+":"+strconv.Itoa(nextPage),
			strconv.Itoa(nextPage),
			slack.NewTextBlockObject(slack.PlainTextType, "더보기", false, false),
		)
		blocks = append(blocks,
			slack.NewDividerBlock(),
			slack.NewActionBlock("task_pagination", btn),
		)
	}

	var fallback string
	if ownerName != "" {
		fallback = fmt.Sprintf("%s의 활성 task %d건", ownerName, total)
	} else {
		fallback = fmt.Sprintf("활성 task %d건", total)
	}
	return blocks, fallback
}

func hasNextPage(page, pageSize, total int) bool {
	return (page+1)*pageSize < total
}

func taskRowBlock(t store.ConsolidatedMessage) slack.Block {
	title := strings.TrimSpace(t.Task)
	if title == "" {
		title = "(제목 없음)"
	}
	meta := buildTaskMeta(t)
	body := "*" + slackEscape(title) + "*"
	if meta != "" {
		body += "\n" + meta
	}

	doneBtn := slack.NewButtonBlockElement(
		SlackActionTaskDone+":"+strconv.FormatInt(int64(t.ID), 10),
		strconv.FormatInt(int64(t.ID), 10),
		slack.NewTextBlockObject(slack.PlainTextType, "완료", false, false),
	)
	doneBtn.Style = slack.StylePrimary

	return slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, body, false, false),
		nil,
		slack.NewAccessory(doneBtn),
	)
}

// taskRowBlockReadOnly renders a task row without the done button for delegated views.
func taskRowBlockReadOnly(t store.ConsolidatedMessage) slack.Block {
	title := strings.TrimSpace(t.Task)
	if title == "" {
		title = "(제목 없음)"
	}
	meta := buildTaskMeta(t)
	body := "*" + slackEscape(title) + "*"
	if meta != "" {
		body += "\n" + meta
	}
	return slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, body, false, false),
		nil, nil,
	)
}

func buildTaskMeta(t store.ConsolidatedMessage) string {
	parts := make([]string, 0, 5)
	if t.Source != "" {
		parts = append(parts, t.Source)
	}
	if t.Room != "" {
		parts = append(parts, "#"+t.Room)
	}
	if t.Category != "" {
		parts = append(parts, t.Category)
	}
	if t.Requester != "" {
		parts = append(parts, "from "+t.Requester)
	}
	if t.Assignee != "" {
		parts = append(parts, "to "+t.Assignee)
	}
	if t.Deadline != "" {
		parts = append(parts, "due "+t.Deadline)
	}
	if len(parts) == 0 {
		return ""
	}
	return "_" + slackEscape(strings.Join(parts, " · ")) + "_"
}

// Why: Slack mrkdwn treats <, >, & as control chars (autolink, entity); escape per
//
//	api.slack.com/reference/surfaces/formatting#escaping to prevent broken rendering
//	from task titles containing email <addr@host> or "&" symbols.
func slackEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// ParseSlackActionID splits a Slack action_id into (kind, arg).
// Returns ok=false on malformed input so the handler can ignore unknown buttons.
func ParseSlackActionID(actionID string) (kind, arg string, ok bool) {
	idx := strings.Index(actionID, ":")
	if idx <= 0 || idx == len(actionID)-1 {
		return "", "", false
	}
	return actionID[:idx], actionID[idx+1:], true
}
