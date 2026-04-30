package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

type WeeklyReportSlack interface {
	SendDM(ctx context.Context, slackUserID, text string) error
	LookupSlackIDByEmail(email string) (string, error)
}

type WeeklyReportConfig struct {
	RecipientEmail string
	Hour           int
	Timezone       string
	Language       string
	PollInterval   time.Duration
	PollTimeout    time.Duration
}

type WeeklyReportService struct {
	Slack   WeeklyReportSlack
	Reports *ReportsService
	Notion  *NotionExporter
	Config  WeeklyReportConfig
	nowFn   func() time.Time
}

func NewWeeklyReportService(slack WeeklyReportSlack, reports *ReportsService, notion *NotionExporter, cfg WeeklyReportConfig) *WeeklyReportService {
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
		Slack: slack, Reports: reports, Notion: notion, Config: cfg,
		nowFn: func() time.Time { return time.Now() },
	}
}

func (s *WeeklyReportService) Dispatch(ctx context.Context) error {
	if s == nil || s.Slack == nil || s.Reports == nil || s.Notion == nil || s.Config.RecipientEmail == "" {
		return nil
	}
	slackID, err := s.ensureSlackID(ctx)
	if err != nil {
		return fmt.Errorf("weekly: slack id: %w", err)
	}
	loc, err := time.LoadLocation(s.Config.Timezone)
	if err != nil {
		loc = time.UTC
	}
	start, end := computeWeekWindow(s.nowFn().In(loc))

	placeholder, err := s.Reports.GenerateReport(ctx, s.Config.RecipientEmail, start, end, s.Config.Language, nil, nil)
	if err != nil {
		return fmt.Errorf("weekly: generate: %w", err)
	}
	completed, err := s.waitForCompletion(ctx, placeholder.ID, s.Config.RecipientEmail)
	if err != nil {
		return fmt.Errorf("weekly: wait: %w", err)
	}

	title := fmt.Sprintf("Weekly_%s_%s", start, end)
	url, err := s.Notion.ExportReport(ctx, title, completed.ReportSummary)
	if err != nil {
		return fmt.Errorf("weekly: notion: %w", err)
	}
	text := formatWeeklyDMText(start, end, url)
	if err := s.Slack.SendDM(ctx, slackID, text); err != nil {
		return fmt.Errorf("weekly: slack dm: %w", err)
	}
	return nil
}

// Why: Slack DM silently no-ops when user.slack_id is blank — bootstrap via lookupByEmail on first run.
func (s *WeeklyReportService) ensureSlackID(ctx context.Context) (string, error) {
	user, err := store.GetOrCreateUser(ctx, s.Config.RecipientEmail, "", "")
	if err != nil || user == nil {
		return "", fmt.Errorf("get user %s: %w", s.Config.RecipientEmail, err)
	}
	if id := strings.TrimSpace(user.SlackID); id != "" {
		return id, nil
	}
	id, err := s.Slack.LookupSlackIDByEmail(s.Config.RecipientEmail)
	if err != nil {
		return "", fmt.Errorf("lookup slack id: %w", err)
	}
	if err := store.UpdateUserSlackID(ctx, s.Config.RecipientEmail, id); err != nil {
		logger.Warnf("[WEEKLY] persist slack id failed: %v", err)
	}
	return id, nil
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

func formatWeeklyDMText(start, end, url string) string {
	return fmt.Sprintf(":bar_chart: *Weekly Report* (%s ~ %s)\n%s", start, end, url)
}
