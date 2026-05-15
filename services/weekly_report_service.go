package services

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

type WeeklyReportMailer interface {
	SendWeeklyEmail(ctx context.Context, from, to, subject, body string) (msgID string, err error)
}

type WeeklyReportConfig struct {
	RecipientEmails []string
	Hour            int
	Timezone        string
	Language        string
	PollInterval    time.Duration
	PollTimeout     time.Duration
}

type WeeklyReportService struct {
	Mailer  WeeklyReportMailer
	Reports *ReportsService
	Notion  *NotionExporter
	Config  WeeklyReportConfig
	nowFn   func() time.Time
}

func NewWeeklyReportService(mailer WeeklyReportMailer, reports *ReportsService, notion *NotionExporter, cfg WeeklyReportConfig) *WeeklyReportService {
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
	return &WeeklyReportService{
		Mailer: mailer, Reports: reports, Notion: notion, Config: cfg,
		nowFn: func() time.Time { return time.Now() },
	}
}

func (s *WeeklyReportService) Dispatch(ctx context.Context) error {
	if s == nil || s.Mailer == nil || s.Reports == nil || s.Notion == nil || len(s.Config.RecipientEmails) == 0 {
		return nil
	}
	return s.deliver(ctx, s.Config.RecipientEmails)
}

func (s *WeeklyReportService) DispatchTo(ctx context.Context, recipient string) error {
	if s == nil || s.Mailer == nil || s.Reports == nil || s.Notion == nil || len(s.Config.RecipientEmails) == 0 {
		return fmt.Errorf("weekly: service not configured")
	}
	return s.deliver(ctx, []string{recipient})
}

func (s *WeeklyReportService) deliver(ctx context.Context, recipients []string) error {
	loc, err := time.LoadLocation(s.Config.Timezone)
	if err != nil {
		loc = time.UTC
	}
	start, end := computeWeekWindow(s.nowFn().In(loc))

	primary := s.Config.RecipientEmails[0]
	placeholder, err := s.Reports.GenerateReport(ctx, primary, start, end, s.Config.Language, nil, nil)
	if err != nil {
		return fmt.Errorf("weekly: generate: %w", err)
	}
	completed, err := s.waitForCompletion(ctx, placeholder.ID, primary)
	if err != nil {
		return fmt.Errorf("weekly: wait: %w", err)
	}

	title := fmt.Sprintf("Weekly_%s_%s", start, end)
	url, err := s.Notion.ExportReport(ctx, title, completed.ReportSummary)
	if err != nil {
		return fmt.Errorf("weekly: notion: %w", err)
	}
	subject := formatWeeklyEmailSubject(start, end)
	body := formatWeeklyEmailBody(start, end, url, completed.ReportSummary)
	for _, email := range recipients {
		msgID, err := s.Mailer.SendWeeklyEmail(ctx, primary, email, subject, body)
		if err != nil {
			logger.Warnf("[WEEKLY] send email to %s: %v", email, err)
			continue
		}
		if msgID != "" {
			_ = store.MarkAsProcessed(ctx, store.GetDB(), primary, msgID)
		}
	}
	return nil
}

func (s *WeeklyReportService) waitForCompletion(ctx context.Context, id store.ReportID, email string) (*store.Report, error) {
	return pollUntilReportCompleted(ctx, id, email, s.Config.PollInterval, s.Config.PollTimeout)
}

func pollUntilReportCompleted(ctx context.Context, id store.ReportID, email string, pollInterval, pollTimeout time.Duration) (*store.Report, error) {
	deadline := time.Now().Add(pollTimeout)
	for {
		r, err := store.GetReportByID(ctx, id, email)
		if err != nil {
			return nil, fmt.Errorf("get report %d: %w", id, err)
		}
		switch r.Status {
		case store.ReportStatusCompleted:
			if strings.TrimSpace(r.ReportSummary) == "" {
				return nil, fmt.Errorf("report %d completed but english summary empty", id)
			}
			return r, nil
		case store.ReportStatusFailed:
			return nil, fmt.Errorf("report %d failed", id)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("report %d not completed within %s", id, pollTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// Why: Dispatched Friday 18 KST — the reported week is Sat..Fri ending today.
func computeWeekWindow(now time.Time) (string, string) {
	end := now
	start := end.AddDate(0, 0, -6)
	layout := "2006-01-02"
	return start.Format(layout), end.Format(layout)
}

func formatWeeklyEmailSubject(start, end string) string {
	return fmt.Sprintf("[WR] %s~%s Weekly report", start, end)
}

func formatWeeklyEmailBody(start, end, url, summary string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family:Arial,sans-serif;max-width:720px;margin:0 auto;padding:24px;color:#333">
  <h1 style="color:#1a73e8;border-bottom:2px solid #1a73e8;padding-bottom:8px;font-size:20px">
    Weekly Report: %s ~ %s
  </h1>
  %s
  <hr style="border:none;border-top:1px solid #ddd;margin:24px 0">
  <p style="font-size:13px;color:#666">
    <a href="%s" style="color:#1a73e8">View full report on Notion</a>
  </p>
</body>
</html>`, start, end, mdToEmailHTML(summary), url)
}

func mdToEmailHTML(md string) string {
	var buf strings.Builder
	lines := strings.Split(md, "\n")
	inCode, inList := false, false
	var codeLines []string

	closeList := func() {
		if inList {
			buf.WriteString("</ul>\n")
			inList = false
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inCode {
				content := strings.Join(codeLines, "\n")
				if table := jsonToTable(content); table != "" {
					buf.WriteString(table + "\n")
				} else {
					buf.WriteString(`<pre style="background:#f5f5f5;padding:12px;border-radius:4px;overflow-x:auto;font-size:12px;line-height:1.5">`)
					buf.WriteString(html.EscapeString(content))
					buf.WriteString("</pre>\n")
				}
				codeLines = nil
				inCode = false
			} else {
				closeList()
				inCode = true
			}
			continue
		}
		if inCode {
			codeLines = append(codeLines, line)
			continue
		}
		if strings.HasPrefix(line, "## ") {
			closeList()
			buf.WriteString(fmt.Sprintf(`<h2 style="color:#1a73e8;margin-top:28px;font-size:16px">%s</h2>`+"\n",
				html.EscapeString(strings.TrimPrefix(line, "## "))))
			continue
		}
		if strings.HasPrefix(line, "- ") {
			if !inList {
				buf.WriteString(`<ul style="padding-left:20px">` + "\n")
				inList = true
			}
			buf.WriteString(fmt.Sprintf("<li style=\"margin:4px 0\">%s</li>\n", inlineMD(strings.TrimPrefix(line, "- "))))
			continue
		}
		if line == "---" {
			closeList()
			buf.WriteString(`<hr style="border:none;border-top:1px solid #ddd;margin:16px 0">` + "\n")
			continue
		}
		if strings.TrimSpace(line) == "" {
			closeList()
			continue
		}
		closeList()
		buf.WriteString(fmt.Sprintf("<p style=\"margin:8px 0;line-height:1.6\">%s</p>\n", inlineMD(line)))
	}
	closeList()
	if inCode {
		buf.WriteString("</pre>\n")
	}
	return buf.String()
}

func jsonToTable(s string) string {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &rows); err != nil || len(rows) == 0 {
		return ""
	}
	cols := orderedKeys(rows[0])
	if len(cols) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<table style="width:100%;border-collapse:collapse;font-size:13px;margin:8px 0">`)
	b.WriteString("<tr>")
	for _, c := range cols {
		b.WriteString(fmt.Sprintf(`<th style="background:#1a73e8;color:#fff;padding:8px 10px;text-align:left;white-space:nowrap">%s</th>`,
			html.EscapeString(strings.Title(c))))
	}
	b.WriteString("</tr>")
	for i, row := range rows {
		bg := "#fff"
		if i%2 == 1 {
			bg = "#f8f9fa"
		}
		b.WriteString(fmt.Sprintf(`<tr style="background:%s">`, bg))
		for _, c := range cols {
			val := fmt.Sprintf("%v", row[c])
			b.WriteString(fmt.Sprintf(`<td style="padding:7px 10px;border-bottom:1px solid #eee;vertical-align:top">%s</td>`,
				html.EscapeString(val)))
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
	return b.String()
}

func inlineMD(s string) string {
	var buf strings.Builder
	for len(s) > 0 {
		idx := strings.Index(s, "**")
		if idx < 0 {
			buf.WriteString(html.EscapeString(s))
			break
		}
		buf.WriteString(html.EscapeString(s[:idx]))
		s = s[idx+2:]
		end := strings.Index(s, "**")
		if end < 0 {
			buf.WriteString("**" + html.EscapeString(s))
			break
		}
		buf.WriteString("<strong>" + html.EscapeString(s[:end]) + "</strong>")
		s = s[end+2:]
	}
	return buf.String()
}
