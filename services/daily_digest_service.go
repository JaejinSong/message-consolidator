package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

type DailyDigestSlack interface {
	SendDM(ctx context.Context, slackUserID, text string) error
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

	body := formatDailyDMText(start, end, completed.ReportSummary)
	for _, email := range s.Config.RecipientEmails {
		slackID, err := s.ensureSlackIDFor(ctx, email)
		if err != nil {
			logger.Warnf("[DIGEST] slack id for %s: %v", email, err)
			continue
		}
		if err := s.Slack.SendDM(ctx, slackID, body); err != nil {
			logger.Warnf("[DIGEST] send dm to %s: %v", email, err)
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
