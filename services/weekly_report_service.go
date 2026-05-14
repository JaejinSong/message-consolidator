package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

type WeeklyReportMailer interface {
	SendEmail(ctx context.Context, from, to, subject, body string) error
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
		if err := s.Mailer.SendEmail(ctx, primary, email, subject, body); err != nil {
			logger.Warnf("[WEEKLY] send email to %s: %v", email, err)
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
	return fmt.Sprintf("Weekly Report: %s ~ %s\n\n%s\n\n---\nFull report: %s", start, end, summary, url)
}
