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
	CloudRunMode           bool
	InternalScanSecret     string
	MessageBatchWindow     time.Duration
	DBMaxIdleConns         int
	DBMaxOpenConns         int
	DBKeepAliveInterval    time.Duration
}

func LoadConfig() *Config {
	//Why: Attempts to load environment variables from a local .env file, while allowing a silent fallback to system environment variables for production environments like Cloud Run.
	_ = godotenv.Load(".env")

	//Why: Loads local overrides from .env.local if present, ensuring local development settings take precedence over shared .env settings.
	if _, err := os.Stat(".env.local"); err == nil {
		if err := godotenv.Overload(".env.local"); err != nil {
			logger.Warnf("Failed to load .env.local: %v", err)
		} else {
			logger.Infof("Loaded local overrides from .env.local")
		}
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO"
	}

	geminiAnalysisModel := os.Getenv("GEMINI_ANALYSIS_MODEL")
	if geminiAnalysisModel == "" {
		geminiAnalysisModel = "gemini-3-flash-preview"
	}

	geminiTranslationModel := os.Getenv("GEMINI_TRANSLATION_MODEL")
	if geminiTranslationModel == "" {
		geminiTranslationModel = "gemini-3.1-flash-lite"
	}

	autoArchiveDays := 7
	if daysStr := os.Getenv("AUTO_ARCHIVE_DAYS"); daysStr != "" {
		if days, err := strconv.Atoi(daysStr); err == nil {
			autoArchiveDays = days
		}
	}
	if daysStr := os.Getenv("ARCHIVE_DAYS"); daysStr != "" {
		if days, err := strconv.Atoi(daysStr); err == nil {
			autoArchiveDays = days
		}
	}

	batchWindow := 5 * time.Minute
	if wStr := os.Getenv("MESSAGE_BATCH_WINDOW"); wStr != "" {
		if d, err := time.ParseDuration(wStr); err == nil {
			batchWindow = d
		}
	}

	dbMaxIdle := 1
	if val := os.Getenv("DB_MAX_IDLE_CONNS"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			dbMaxIdle = i
		}
	}

	dbMaxOpen := 25
	if val := os.Getenv("DB_MAX_OPEN_CONNS"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			dbMaxOpen = i
		}
	}

	dbKeepAlive := 8 * time.Second
	if val := os.Getenv("DB_KEEP_ALIVE_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			dbKeepAlive = d
		} else if sec, err := strconv.Atoi(val); err == nil {
			dbKeepAlive = time.Duration(sec) * time.Second
		}
	}

	//Why: Comma-separated company domains used to whitelist internal Google Group traffic past the marketing-header filter,
	// which would otherwise drop every internal mailing-list copy due to the List-Unsubscribe header Google Groups injects.
	var companyDomains []string
	if v := os.Getenv("COMPANY_DOMAINS"); v != "" {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(strings.ToLower(s))
			if s != "" {
				companyDomains = append(companyDomains, s)
			}
		}
	}

	//Why: Telegram App ID is an integer issued by https://my.telegram.org; empty/invalid value disables the Telegram channel gracefully.
	tgAppID := 0
	if v := os.Getenv("TELEGRAM_APP_ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tgAppID = n
		} else {
			logger.Warnf("invalid TELEGRAM_APP_ID: %v", err)
		}
	}

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
		LogLevel:               logLevel,
		GeminiAnalysisModel:    geminiAnalysisModel,
		GeminiTranslationModel: geminiTranslationModel,
		NotionToken:            os.Getenv("NOTION_TOKEN"),
		NotionReportPageID:     os.Getenv("NOTION_REPORT_PAGE_ID"),
		TelegramAppID:          tgAppID,
		TelegramAppHash:        os.Getenv("TELEGRAM_APP_HASH"),
		GmailSkipSenders:       os.Getenv("GMAIL_SKIP_SENDERS"),
		CompanyDomains:         companyDomains,
		AutoArchiveDays:        autoArchiveDays,
		CloudRunMode:           os.Getenv("CLOUD_RUN_MODE") == "true",
		InternalScanSecret:     os.Getenv("INTERNAL_SCAN_SECRET"),
		MessageBatchWindow:     batchWindow,
		DBMaxIdleConns:         dbMaxIdle,
		DBMaxOpenConns:         dbMaxOpen,
		DBKeepAliveInterval:    dbKeepAlive,
	}
}
