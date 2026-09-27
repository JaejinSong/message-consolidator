package scanner

import (
	"context"
	"message-consolidator/ai"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
)

func Init(c *config.Config) {
	cfg = c
	deps.roomLockSvc = services.NewRoomLockService()
	pc := ai.ProviderConfig{
		Provider:                 cfg.AIProvider,
		GeminiAPIKey:             cfg.GeminiAPIKey,
		GeminiAnalysisModel:      cfg.GeminiAnalysisModel,
		GeminiTranslationModel:   cfg.GeminiTranslationModel,
		DeepSeekAPIKey:           cfg.DeepSeekAPIKey,
		DeepSeekBaseURL:          cfg.DeepSeekBaseURL,
		DeepSeekFilterModel:      cfg.DeepSeekFilterModel,
		DeepSeekAnalysisModel:    cfg.DeepSeekAnalysisModel,
		DeepSeekTranslationModel: cfg.DeepSeekTranslationModel,
		DeepSeekReportModel:      cfg.DeepSeekReportModel,
	}
	if pc.Enabled() {
		gc, err := ai.NewAIClient(context.Background(), pc)
		if err != nil {
			logger.Errorf("[SCAN] failed to init AI client (%s): %v", cfg.AIProvider, err)
			return
		}
		deps.gClient = gc
		transSvc := services.NewTranslationService(deps.gClient)
		deps.tasksSvc = services.NewTasksService(transSvc, deps.gClient)
		deps.completionSvc = services.NewCompletionService(deps.gClient, &services.DefaultTaskStore{}, deps.tasksSvc, store.GetDB())
		deps.filterSvc = ai.NewGeminiLiteFilter(deps.gClient)
	}
	if cfg.SlackToken != "" {
		deps.slackClient = channels.NewSlackClient(cfg.SlackToken)
		deps.reminderSvc = services.NewReminderService(deps.slackClient, cfg.ReminderWindowsHours)
	}
	// Why: candidate proposal is chip-only and must work without Slack; digest is nil-Slack-safe.
	// Typed-nil guard: wrapping a nil *SlackClient in the interface would defeat the nil check.
	var exclusionSlack services.SlackPoster
	if deps.slackClient != nil {
		exclusionSlack = deps.slackClient
	}
	deps.exclusionSvc = services.NewExclusionService(exclusionSlack)
	deps.pastEventSvc = services.NewPastEventService()
}

// Why: DailyDigestService needs reportsSvc, built post-Init in main.initAIServices.
func WireDailyDigest(reportsSvc *services.ReportsService) {
	if cfg == nil || !cfg.DailyDigestEnabled || reportsSvc == nil || deps.slackClient == nil {
		return
	}
	if len(cfg.DailyDigestRecipientEmails) == 0 {
		logger.Warnf("[DIGEST] recipient emails not set")
		return
	}
	svc := services.NewDailyDigestService(deps.slackClient, reportsSvc, services.DailyDigestConfig{
		RecipientEmails: cfg.DailyDigestRecipientEmails,
		Hour:            cfg.DailyDigestHour,
		Timezone:        cfg.DailyDigestTimezone,
		Language:        cfg.DailyDigestLanguage,
	})
	svc.Notion = services.NewNotionExporter(cfg.NotionToken, cfg.NotionReportPageID)
	deps.digestSvc = svc
}

type gmailMailer struct{}

func (g gmailMailer) SendWeeklyEmail(ctx context.Context, from, to, subject, body string) (string, error) {
	return channels.SendGmailEmailWithOrigin(ctx, from, to, subject, body)
}

// Why: WeeklyReportService needs reportsSvc which is built post-Init in main.go's initAIServices.
func WireWeeklyReport(reportsSvc *services.ReportsService) {
	if cfg == nil || !cfg.WeeklyReportEnabled || reportsSvc == nil {
		return
	}
	if len(cfg.WeeklyReportRecipientEmails) == 0 {
		logger.Warnf("[WEEKLY] recipient email not set")
		return
	}
	notion := services.NewNotionExporter(cfg.NotionToken, cfg.NotionReportPageID)
	if !notion.Enabled() {
		logger.Warnf("[WEEKLY] notion not configured")
		return
	}
	deps.weeklyReportSvc = services.NewWeeklyReportService(gmailMailer{}, reportsSvc, notion, services.WeeklyReportConfig{
		RecipientEmails: cfg.WeeklyReportRecipientEmails,
		Hour:            cfg.WeeklyReportHour,
		Timezone:        cfg.WeeklyReportTimezone,
		Language:        cfg.WeeklyReportLang,
	})
}
