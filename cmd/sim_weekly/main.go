package main

import (
	"context"
	"log"
	"message-consolidator/ai"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"os"

	"github.com/whatap/go-api/trace"
)

func main() {
	logger.InitLogging()
	cfg := config.LoadConfig()
	logger.SetLevel(cfg.LogLevel)
	trace.Init(map[string]string{})
	defer trace.Shutdown()

	ctx := context.Background()
	if err := store.InitDB(ctx, cfg); err != nil {
		log.Fatalf("DB init: %v", err)
	}

	if cfg.GeminiAPIKey == "" || cfg.SlackToken == "" || cfg.NotionToken == "" {
		log.Fatalf("missing required env: GEMINI_API_KEY/SLACK_TOKEN/NOTION_TOKEN")
	}
	recipient := cfg.WeeklyReportRecipientEmail
	if recipient == "" {
		recipient = os.Getenv("WEEKLY_REPORT_RECIPIENT_EMAIL")
	}
	if recipient == "" {
		log.Fatalf("missing WEEKLY_REPORT_RECIPIENT_EMAIL")
	}

	gClient, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		log.Fatalf("gemini: %v", err)
	}
	transSvc := services.NewTranslationService(gClient)
	summarizer := services.NewFlashSingleSummarizer(gClient)
	reportsSvc := services.NewReportsService(summarizer, gClient, transSvc, services.ReportConfig{CutoffSize: services.DefaultReportCutoffSize})

	slack := channels.NewSlackClient(cfg.SlackToken)
	notion := services.NewNotionExporter(cfg.NotionToken, cfg.NotionReportPageID)
	if !notion.Enabled() {
		log.Fatalf("notion not configured")
	}

	svc := services.NewWeeklyReportService(slack, reportsSvc, notion, services.WeeklyReportConfig{
		RecipientEmail: recipient,
		Hour:           cfg.WeeklyReportHour,
		Timezone:       cfg.WeeklyReportTimezone,
		Language:       cfg.WeeklyReportLang,
	})

	log.Printf("[SIM-WEEKLY] dispatching for %s ...", recipient)
	if err := svc.Dispatch(ctx); err != nil {
		log.Fatalf("dispatch: %v", err)
	}
	log.Printf("[SIM-WEEKLY] done — check Slack DM")
}
