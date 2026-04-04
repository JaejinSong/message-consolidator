package config

import (
	"message-consolidator/logger"
	"os"
	"strconv"

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
	AutoArchiveDays        int
	CloudRunMode           bool
	InternalScanSecret     string
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
		GmailSkipSenders:       os.Getenv("GMAIL_SKIP_SENDERS"),
		AutoArchiveDays:        autoArchiveDays,
		CloudRunMode:           os.Getenv("CLOUD_RUN_MODE") == "true",
		InternalScanSecret:     os.Getenv("INTERNAL_SCAN_SECRET"),
	}
}
