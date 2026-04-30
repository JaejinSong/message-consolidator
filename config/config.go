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
	ReminderEnabled           bool
	ReminderWindowsHours      []int
	DailyDigestEnabled         bool
	DailyDigestRecipientEmails []string
	DailyDigestHour            int
	DailyDigestTimezone        string
	DailyDigestLanguage        string
	WeeklyReportEnabled         bool
	WeeklyReportRecipientEmails []string
	WeeklyReportHour            int
	WeeklyReportTimezone       string
	WeeklyReportLang           string
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
		// Why: Turso server-side closes idle libsql streams after 10s; 7s leaves 3s margin for jitter/GC.
		DBKeepAliveInterval:  envDurationOrSeconds("DB_KEEP_ALIVE_INTERVAL", 7*time.Second),
		ReminderEnabled:           parseBoolEnv("REMINDER_ENABLED", false),
		ReminderWindowsHours:      parseIntCSV(os.Getenv("REMINDER_WINDOWS_HOURS"), []int{24, 1}),
		DailyDigestEnabled:         parseBoolEnv("DAILY_DIGEST_ENABLED", false),
		DailyDigestRecipientEmails: splitCSV(os.Getenv("DAILY_DIGEST_RECIPIENT_EMAIL")),
		DailyDigestHour:            envInt("DAILY_DIGEST_HOUR", 18),
		DailyDigestTimezone:        envOr("DAILY_DIGEST_TIMEZONE", "Asia/Seoul"),
		DailyDigestLanguage:        envOr("DAILY_DIGEST_LANGUAGE", "en"),
		WeeklyReportEnabled:         parseBoolEnv("WEEKLY_REPORT_ENABLED", false),
		WeeklyReportRecipientEmails: splitCSV(os.Getenv("WEEKLY_REPORT_RECIPIENT_EMAIL")),
		WeeklyReportHour:            envInt("WEEKLY_REPORT_HOUR", 18),
		WeeklyReportTimezone:        envOr("WEEKLY_REPORT_TIMEZONE", "Asia/Seoul"),
		WeeklyReportLang:            envOr("WEEKLY_REPORT_LANG", "en"),
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

// parseBoolEnv reads an env var and returns true for "true"/"1"/"yes" (case-insensitive).
func parseBoolEnv(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return fallback
}

// parseIntCSV parses a comma-separated string of integers. Returns fallback on empty or parse error.
func parseIntCSV(raw string, fallback []int) []int {
	if raw == "" {
		return fallback
	}
	var out []int
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return fallback
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
