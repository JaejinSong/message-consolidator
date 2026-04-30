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

	if cfg.GeminiAPIKey == "" || cfg.SlackToken == "" {
		log.Fatalf("missing required env: GEMINI_API_KEY/SLACK_TOKEN")
	}

	// Why: SIM_DAILY_RECIPIENT overrides config to scope test DM to a single inbox.
	recipient := os.Getenv("SIM_DAILY_RECIPIENT")
	if recipient == "" && len(cfg.DailyDigestRecipientEmails) > 0 {
		recipient = cfg.DailyDigestRecipientEmails[0]
	}
	if recipient == "" {
		log.Fatalf("missing SIM_DAILY_RECIPIENT or DAILY_DIGEST_RECIPIENT_EMAIL")
	}

	gClient, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		log.Fatalf("gemini: %v", err)
	}
	transSvc := services.NewTranslationService(gClient)
	summarizer := services.NewFlashSingleSummarizer(gClient)
	reportsSvc := services.NewReportsService(summarizer, gClient, transSvc, services.ReportConfig{CutoffSize: services.DefaultReportCutoffSize})

	slack := channels.NewSlackClient(cfg.SlackToken)
	svc := services.NewDailyDigestService(slack, reportsSvc, services.DailyDigestConfig{
		RecipientEmails: []string{recipient},
		Hour:            cfg.DailyDigestHour,
		Timezone:        cfg.DailyDigestTimezone,
		Language:        cfg.DailyDigestLanguage,
	})

	log.Printf("[SIM-DAILY] dispatching for %s ...", recipient)
	if err := svc.Dispatch(ctx); err != nil {
		log.Fatalf("dispatch: %v", err)
	}
	log.Printf("[SIM-DAILY] done — check Slack DM")
}
