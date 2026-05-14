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
	"strings"

	"github.com/whatap/go-api/trace"
)

type gmailMailer struct{}

func (g gmailMailer) SendEmail(ctx context.Context, from, to, subject, body string) error {
	return channels.SendGmailEmail(ctx, from, to, subject, body)
}

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

	if cfg.GeminiAPIKey == "" || cfg.NotionToken == "" {
		log.Fatalf("missing required env: GEMINI_API_KEY/NOTION_TOKEN")
	}
	channels.SetupGmailOAuth(cfg)

	recipients := cfg.WeeklyReportRecipientEmails
	if v := os.Getenv("SIM_WEEKLY_RECIPIENT"); v != "" {
		recipients = splitCSV(v)
	}
	if len(recipients) == 0 {
		log.Fatalf("missing WEEKLY_REPORT_RECIPIENT_EMAIL")
	}

	gClient, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		log.Fatalf("gemini: %v", err)
	}
	transSvc := services.NewTranslationService(gClient)
	summarizer := services.NewFlashSingleSummarizer(gClient)
	reportsSvc := services.NewReportsService(summarizer, gClient, transSvc, services.ReportConfig{CutoffSize: services.DefaultReportCutoffSize})

	notion := services.NewNotionExporter(cfg.NotionToken, cfg.NotionReportPageID)
	if !notion.Enabled() {
		log.Fatalf("notion not configured")
	}

	svc := services.NewWeeklyReportService(gmailMailer{}, reportsSvc, notion, services.WeeklyReportConfig{
		RecipientEmails: recipients,
		Hour:            cfg.WeeklyReportHour,
		Timezone:        cfg.WeeklyReportTimezone,
		Language:        cfg.WeeklyReportLang,
	})

	log.Printf("[SIM-WEEKLY] dispatching for %v ...", recipients)
	if err := svc.Dispatch(ctx); err != nil {
		log.Fatalf("dispatch: %v", err)
	}
	log.Printf("[SIM-WEEKLY] done — check email inbox")
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
