package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

type DailyDigestSlack interface {
	SendDMBlocks(ctx context.Context, slackUserID string, blocks []slack.Block, fallback string) error
	LookupSlackIDByEmail(email string) (string, error)
}

type DailyDigestConfig struct {
	RecipientEmails []string
	Hour            int
	Timezone        string
	Language        string
	PollInterval    time.Duration
	PollTimeout     time.Duration
}

type DailyDigestService struct {
	Slack   DailyDigestSlack
	Reports *ReportsService
	Config  DailyDigestConfig
	Notion  *NotionExporter
	nowFn   func() time.Time
}

func NewDailyDigestService(slack DailyDigestSlack, reports *ReportsService, cfg DailyDigestConfig) *DailyDigestService {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 7 * time.Second
	}
	if cfg.PollTimeout == 0 {
		cfg.PollTimeout = 11 * time.Minute
	}
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Seoul"
	}
	return &DailyDigestService{
		Slack: slack, Reports: reports, Config: cfg,
		nowFn: func() time.Time { return time.Now() },
	}
}

func (s *DailyDigestService) Dispatch(ctx context.Context) error {
	if s == nil || s.Slack == nil || s.Reports == nil || len(s.Config.RecipientEmails) == 0 {
		return nil
	}
	loc, err := time.LoadLocation(s.Config.Timezone)
	if err != nil {
		loc = time.UTC
	}
	start, end := computeDailyWindow(s.nowFn().In(loc))

	primary := s.Config.RecipientEmails[0]
	placeholder, err := s.Reports.GenerateReport(ctx, primary, start, end, s.Config.Language, nil, nil)
	if err != nil {
		return fmt.Errorf("daily: generate: %w", err)
	}
	completed, err := pollUntilReportCompleted(ctx, placeholder.ID, primary, s.Config.PollInterval, s.Config.PollTimeout)
	if err != nil {
		return fmt.Errorf("daily: wait: %w", err)
	}

	blocks := formatDailyDMBlocks(start, end, completed.ReportSummary)
	fallback := formatDailyDMText(start, end, completed.ReportSummary)
	for _, email := range s.Config.RecipientEmails {
		slackID, err := s.ensureSlackIDFor(ctx, email)
		if err != nil {
			logger.Warnf("[DIGEST] slack id for %s: %v", email, err)
			continue
		}
		if err := s.Slack.SendDMBlocks(ctx, slackID, blocks, fallback); err != nil {
			logger.Warnf("[DIGEST] send dm to %s: %v", email, err)
		}
	}

	if s.Notion != nil && s.Notion.Enabled() {
		title := fmt.Sprintf("Daily Report %s", end)
		if _, err := s.Notion.ExportReport(ctx, title, completed.ReportSummary); err != nil {
			logger.Warnf("[DIGEST] notion export: %v", err)
		}
	}
	return nil
}

// Why: Slack DM silently no-ops when user.slack_id is blank — bootstrap via lookupByEmail on first send.
func (s *DailyDigestService) ensureSlackIDFor(ctx context.Context, email string) (string, error) {
	user, err := store.GetOrCreateUser(ctx, email, "", "")
	if err != nil || user == nil {
		return "", fmt.Errorf("get user %s: %w", email, err)
	}
	if id := strings.TrimSpace(user.SlackID); id != "" {
		return id, nil
	}
	id, err := s.Slack.LookupSlackIDByEmail(email)
	if err != nil {
		return "", fmt.Errorf("lookup slack id: %w", err)
	}
	if err := store.UpdateUserSlackID(ctx, email, id); err != nil {
		logger.Warnf("[DIGEST] persist slack id failed: %v", err)
	}
	return id, nil
}

// Why: working-day digest spans calendar dates in KST. Mon = Sat..Mon (Sat/Sun no-send
// accumulates weekend pendings into Monday's window); Tue..Fri = today only.
func computeDailyWindow(now time.Time) (string, string) {
	layout := "2006-01-02"
	end := now.Format(layout)
	if now.Weekday() == time.Monday {
		return now.AddDate(0, 0, -2).Format(layout), end
	}
	return end, end
}

func formatDailyDMText(start, end, summary string) string {
	if start == end {
		return fmt.Sprintf(":sunrise: *Daily Report* (%s)\n\n%s", start, summary)
	}
	return fmt.Sprintf(":sunrise: *Daily Report* (%s ~ %s)\n\n%s", start, end, summary)
}

// Why: AI emits any tabular section (Activity, Stalled Tasks, ...) as a fenced
// ```json [...]``` block that Slack mrkdwn renders verbatim; capture every block so we
// can replace it with structured Block Kit fields without touching the prompt.
var dailyJSONRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(\\[.*?\\])\\s*```")

// Why: encoding/json drops map insertion order when unmarshaling into map[string]any;
// recover the AI's intended field order by scanning the raw JSON for "key": tokens.
var jsonKeyOrderRe = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"\s*:`)

func formatDailyDMBlocks(start, end, summary string) []slack.Block {
	title := fmt.Sprintf(":sunrise: Daily Report (%s)", start)
	if start != end {
		title = fmt.Sprintf(":sunrise: Daily Report (%s ~ %s)", start, end)
	}
	blocks := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, title, true, false)),
	}

	cursor := 0
	for _, loc := range dailyJSONRe.FindAllStringSubmatchIndex(summary, -1) {
		blocks = appendMrkdwnSections(blocks, summary[cursor:loc[0]])
		blocks = appendJSONArrayBlocks(blocks, summary[loc[2]:loc[3]])
		cursor = loc[1]
	}
	blocks = appendMrkdwnSections(blocks, summary[cursor:])
	return blocks
}

func appendJSONArrayBlocks(blocks []slack.Block, raw string) []slack.Block {
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil || len(items) == 0 {
		return appendMrkdwnChunks(blocks, "```\n"+strings.TrimSpace(raw)+"\n```")
	}
	// Why: stalled tasks carry "days", activity carries "customer" — both render as tables.
	if _, ok := items[0]["days"]; ok {
		return appendStalledTasksTable(blocks, items)
	}
	if _, ok := items[0]["customer"]; ok {
		return appendActivityTable(blocks, items)
	}
	keyOrder := extractJSONKeyOrder(raw)
	for _, item := range items {
		blocks = appendJSONItemBlocks(blocks, item, keyOrder)
		blocks = append(blocks, slack.NewDividerBlock())
	}
	return blocks
}

func appendActivityTable(blocks []slack.Block, items []map[string]any) []slack.Block {
	elems := make([]slack.RichTextElement, 0, len(items))
	for _, item := range items {
		customer := stringifyJSONValue(item["customer"])
		count := stringifyJSONValue(item["count"])
		summary := stringifyJSONValue(item["summary"])
		line := slack.NewRichTextSection(
			slack.NewRichTextSectionTextElement(customer, &slack.RichTextSectionTextStyle{Bold: true}),
			slack.NewRichTextSectionTextElement("  "+count+"건  ", nil),
			slack.NewRichTextSectionTextElement(summary, nil),
		)
		elems = append(elems, line)
	}
	return append(blocks, slack.NewRichTextBlock("", slack.NewRichTextList(slack.RTEListBullet, 0, elems...)))
}

func stalledTaskCell(text string, bold bool) *slack.RichTextBlock {
	style := (*slack.RichTextSectionTextStyle)(nil)
	if bold {
		style = &slack.RichTextSectionTextStyle{Bold: true}
	}
	return &slack.RichTextBlock{
		Type: slack.MBTRichText,
		Elements: []slack.RichTextElement{
			slack.NewRichTextSection(slack.NewRichTextSectionTextElement(text, style)),
		},
	}
}

func appendStalledTasksTable(blocks []slack.Block, items []map[string]any) []slack.Block {
	headers := []string{"Source", "Requester", "Assignee", "Status", "Days", "Task"}
	tbl := slack.NewTableBlock("")
	headerRow := make([]*slack.RichTextBlock, len(headers))
	for i, h := range headers {
		headerRow[i] = stalledTaskCell(h, true)
	}
	tbl.Rows = append(tbl.Rows, headerRow)
	for _, item := range items {
		src := stringifyJSONValue(item["source"])
		req := stringifyJSONValue(item["requester"])
		asg := stringifyJSONValue(item["assignee"])
		days := stringifyJSONValue(item["days"]) + "일"
		task := stringifyJSONValue(item["task"])
		tbl.AddRow(
			stalledTaskCell(src, false),
			stalledTaskCell(req, false),
			stalledTaskCell(asg, false),
			stalledTaskCell("STALLED", false),
			stalledTaskCell(days, false),
			stalledTaskCell(task, false),
		)
	}
	return append(blocks, tbl)
}

// Why: short scalars belong in the 2-column fields grid, but anything multi-line or
// long (>80 chars) wraps awkwardly inside fields — promote those to their own section.
func appendJSONItemBlocks(blocks []slack.Block, item map[string]any, keyOrder []string) []slack.Block {
	const inlineFieldLimit = 80
	const maxFields = 10

	var fields []*slack.TextBlockObject
	var bodyLines []string
	for _, k := range orderItemKeys(item, keyOrder) {
		v := stringifyJSONValue(item[k])
		label := titleizeKey(k)
		if len(v) > inlineFieldLimit || strings.Contains(v, "\n") {
			bodyLines = append(bodyLines, fmt.Sprintf("*%s*: %s", label, v))
			continue
		}
		if len(fields) < maxFields {
			fields = append(fields, slack.NewTextBlockObject(slack.MarkdownType,
				fmt.Sprintf("*%s*\n%s", label, v), false, false))
		}
	}
	if len(fields) > 0 {
		blocks = append(blocks, slack.NewSectionBlock(nil, fields, nil))
	}
	for _, line := range bodyLines {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, line, false, false),
			nil, nil,
		))
	}
	return blocks
}

func extractJSONKeyOrder(raw string) []string {
	seen := make(map[string]bool)
	var order []string
	for _, m := range jsonKeyOrderRe.FindAllStringSubmatch(raw, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			order = append(order, m[1])
		}
	}
	return order
}

func orderItemKeys(item map[string]any, preferred []string) []string {
	out := make([]string, 0, len(item))
	seen := make(map[string]bool, len(item))
	for _, k := range preferred {
		if _, ok := item[k]; ok && !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}
	leftover := make([]string, 0)
	for k := range item {
		if !seen[k] {
			leftover = append(leftover, k)
		}
	}
	sort.Strings(leftover)
	return append(out, leftover...)
}

func stringifyJSONValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

// Why: AI emits keys in snake_case/camelCase ("source_ts", "customerName"); a single
// uppercase-first pass keeps the Slack label readable without requiring a full word-split.
func titleizeKey(k string) string {
	if k == "" {
		return k
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

// Why: AI emits sections as `## [Title]` which Slack mrkdwn renders as literal "## "
// prefix; promote each marker to a Block Kit header so Overview/Insights/Stalled get
// the same visual weight as Activity.
var sectionHeaderRe = regexp.MustCompile(`(?m)^##\s*\[?([^\]\n]+?)\]?\s*$`)

func appendMrkdwnSections(blocks []slack.Block, text string) []slack.Block {
	text = strings.TrimSpace(text)
	if text == "" {
		return blocks
	}
	matches := sectionHeaderRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return appendMrkdwnChunks(blocks, text)
	}
	if intro := strings.TrimSpace(text[:matches[0][0]]); intro != "" {
		blocks = appendMrkdwnChunks(blocks, intro)
	}
	for i, m := range matches {
		title := strings.TrimSpace(text[m[2]:m[3]])
		bodyEnd := len(text)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		body := strings.TrimSpace(text[m[1]:bodyEnd])
		blocks = append(blocks, slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, title, false, false),
		))
		if body != "" {
			blocks = appendMrkdwnChunks(blocks, body)
		}
	}
	return blocks
}

// Why: Slack section blocks cap mrkdwn text at 3000 chars; chunk on paragraph boundaries
// so long bodies don't trigger invalid_blocks errors mid-dispatch.
func appendMrkdwnChunks(blocks []slack.Block, text string) []slack.Block {
	const maxSectionChars = 2900
	for _, chunk := range chunkByParagraph(text, maxSectionChars) {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, chunk, false, false),
			nil, nil,
		))
	}
	return blocks
}

func chunkByParagraph(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	var buf strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		if buf.Len()+len(para)+2 > limit && buf.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(buf.String()))
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(para)
	}
	if buf.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(buf.String()))
	}
	return chunks
}
