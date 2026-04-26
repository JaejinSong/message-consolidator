package config

import (
	"message-consolidator/logger"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	SlackToken             string
	GeminiAPIKey           string
	GoogleClientID         string
	GoogleClientSecret     string
	AuthSecret             string
	AuthDisabled           bool
	AppBaseURL             string
	TursoURL               string
	TursoToken             string
	TursoSyncURL           string
	TursoSyncInterval      string
	GeminiAnalysisModel    string
	GeminiTranslationModel string
	LogLevel               string
	GmailSkipSenders       string
	CompanyDomains         []string
	AutoArchiveDays        int
	NotionToken          string
	NotionReportPageID   string
	TelegramAppID          int
	TelegramAppHash        string
	InternalScanSecret     string
	MessageBatchWindow     time.Duration
	DBMaxIdleConns         int
	DBMaxOpenConns         int
	DBKeepAliveInterval    time.Duration
	ScannerTickInterval    time.Duration
	SlackSweepInterval     time.Duration
}

func LoadConfig() *Config {
	loadDotenv()
	return &Config{
		SlackToken:             os.Getenv("SLACK_TOKEN"),
		GeminiAPIKey:           os.Getenv("GEMINI_API_KEY"),
		GoogleClientID:         os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:     os.Getenv("GOOGLE_CLIENT_SECRET"),
		AuthSecret:             os.Getenv("AUTH_SECRET"),
		AuthDisabled:           os.Getenv("AUTH_DISABLED") == "true",
		AppBaseURL:             os.Getenv("APP_BASE_URL"),
		TursoURL:               os.Getenv("TURSO_DATABASE_URL"),
		TursoToken:             os.Getenv("TURSO_AUTH_TOKEN"),
		TursoSyncURL:           os.Getenv("TURSO_SYNC_URL"),
		TursoSyncInterval:      os.Getenv("TURSO_SYNC_INTERVAL"),
		LogLevel:               envOr("LOG_LEVEL", "INFO"),
		GeminiAnalysisModel:    envOr("GEMINI_ANALYSIS_MODEL", "gemini-3-flash-preview"),
		GeminiTranslationModel: envOr("GEMINI_TRANSLATION_MODEL", "gemini-3.1-flash-lite"),
		NotionToken:            os.Getenv("NOTION_TOKEN"),
		NotionReportPageID:     os.Getenv("NOTION_REPORT_PAGE_ID"),
		TelegramAppID:          envInt("TELEGRAM_APP_ID", 0),
		TelegramAppHash:        os.Getenv("TELEGRAM_APP_HASH"),
		GmailSkipSenders:       os.Getenv("GMAIL_SKIP_SENDERS"),
		CompanyDomains:         splitCSV(os.Getenv("COMPANY_DOMAINS")),
		AutoArchiveDays:        envIntFirst([]string{"AUTO_ARCHIVE_DAYS", "ARCHIVE_DAYS"}, 7),
		InternalScanSecret:     os.Getenv("INTERNAL_SCAN_SECRET"),
		MessageBatchWindow:     envDuration("MESSAGE_BATCH_WINDOW", 5*time.Minute),
		DBMaxIdleConns:         envInt("DB_MAX_IDLE_CONNS", 1),
		DBMaxOpenConns:         envInt("DB_MAX_OPEN_CONNS", 25),
		DBKeepAliveInterval:    envDurationOrSeconds("DB_KEEP_ALIVE_INTERVAL", 8*time.Second),
		// Why: 59s (vs round 60s) avoids harmonic resonance with 1-minute upstream cron/poller cadences.
		ScannerTickInterval: envDuration("SCANNER_TICK_INTERVAL", 59*time.Second),
		// Why: Slack thread sweep runs alongside the 59s scanner — 5m offsets reduce load spikes and matches Slack rate-limit headroom.
		SlackSweepInterval: envDuration("SLACK_SWEEP_INTERVAL", 5*time.Minute),
	}
}

//Why: .env loads with silent fallback (env vars may be injected by host/Docker); .env.local overrides for local-only secrets.
func loadDotenv() {
	_ = godotenv.Load(".env")
	if _, err := os.Stat(".env.local"); err != nil {
		return
	}
	if err := godotenv.Overload(".env.local"); err != nil {
		logger.Warnf("Failed to load .env.local: %v", err)
		return
	}
	logger.Infof("Loaded local overrides from .env.local")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		if key == "TELEGRAM_APP_ID" {
			logger.Warnf("invalid TELEGRAM_APP_ID: %v", err)
		}
		return fallback
	}
	return n
}

//Why: AUTO_ARCHIVE_DAYS and ARCHIVE_DAYS are aliases — last non-empty value wins.
func envIntFirst(keys []string, fallback int) int {
	value := fallback
	for _, k := range keys {
		value = envInt(k, value)
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return fallback
}

//Why: DB_KEEP_ALIVE_INTERVAL accepts either Go duration ("8s") or bare seconds ("8") for ops convenience.
func envDurationOrSeconds(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return time.Duration(sec) * time.Second
	}
	return fallback
}

//Why: Comma-separated values are normalized lower-cased and trimmed; empty entries dropped.
func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
