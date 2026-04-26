package config

import (
	"context"
	"message-consolidator/db"
	"message-consolidator/logger"
	"strconv"
	"strings"
	"time"
)

// SettingsLoaderFunc loads all admin-managed settings rows. Wired at boot from store.LoadAllSettings.
// Why: a function (not interface) keeps the config package's only store/ dependency lazy at the
// caller boundary — no DI plumbing needed for a single one-off boot-time call.
type SettingsLoaderFunc func(ctx context.Context) ([]db.AppSetting, error)

// OverlayFromDB merges admin-managed values from app_settings into cfg, taking precedence over .env.
// Empty values are ignored so callers can clear a row to fall back to the .env default.
// Why: registered keys only — anything not in Registry is silently skipped (whitelist gate).
func OverlayFromDB(ctx context.Context, cfg *Config, load SettingsLoaderFunc) error {
	if cfg == nil || load == nil {
		return nil
	}
	rows, err := load(ctx)
	if err != nil {
		return err
	}
	values := make(map[string]string, len(rows))
	for _, r := range rows {
		if v := strings.TrimSpace(r.Value); v != "" {
			values[r.Key] = v
		}
	}
	if len(values) == 0 {
		return nil
	}
	applyOverlay(cfg, values)
	logger.Infof("[CONFIG] Applied %d DB-overlaid settings", len(values))
	return nil
}

// applyOverlay maps each known key onto the corresponding Config field.
// Why: explicit switch (no reflection) keeps the override surface auditable and lint-friendly.
func applyOverlay(cfg *Config, v map[string]string) {
	for key, raw := range v {
		def := FindDef(key)
		if def == nil {
			continue
		}
		assignField(cfg, key, raw)
	}
}

func assignField(cfg *Config, key, raw string) {
	switch key {
	case "SLACK_TOKEN":
		cfg.SlackToken = raw
	case "GEMINI_API_KEY":
		cfg.GeminiAPIKey = raw
	case "GOOGLE_CLIENT_ID":
		cfg.GoogleClientID = raw
	case "GOOGLE_CLIENT_SECRET":
		cfg.GoogleClientSecret = raw
	case "AUTH_SECRET":
		cfg.AuthSecret = raw
	case "AUTH_DISABLED":
		cfg.AuthDisabled = parseBool(raw)
	case "APP_BASE_URL":
		cfg.AppBaseURL = raw
	case "TURSO_DATABASE_URL":
		cfg.TursoURL = raw
	case "TURSO_AUTH_TOKEN":
		cfg.TursoToken = raw
	case "TURSO_SYNC_URL":
		cfg.TursoSyncURL = raw
	case "TURSO_SYNC_INTERVAL":
		cfg.TursoSyncInterval = raw
	case "GEMINI_ANALYSIS_MODEL":
		cfg.GeminiAnalysisModel = raw
	case "GEMINI_TRANSLATION_MODEL":
		cfg.GeminiTranslationModel = raw
	case "LOG_LEVEL":
		cfg.LogLevel = raw
	case "GMAIL_SKIP_SENDERS":
		cfg.GmailSkipSenders = raw
	case "COMPANY_DOMAINS":
		cfg.CompanyDomains = splitCSV(raw)
	case "ARCHIVE_DAYS":
		if n, err := strconv.Atoi(raw); err == nil {
			cfg.AutoArchiveDays = n
		}
	case "NOTION_TOKEN":
		cfg.NotionToken = raw
	case "NOTION_REPORT_PAGE_ID":
		cfg.NotionReportPageID = raw
	case "TELEGRAM_APP_ID":
		if n, err := strconv.Atoi(raw); err == nil {
			cfg.TelegramAppID = n
		}
	case "TELEGRAM_APP_HASH":
		cfg.TelegramAppHash = raw
	case "INTERNAL_SCAN_SECRET":
		cfg.InternalScanSecret = raw
	case "MESSAGE_BATCH_WINDOW":
		if d, err := time.ParseDuration(raw); err == nil {
			cfg.MessageBatchWindow = d
		}
	case "DB_MAX_IDLE_CONNS":
		if n, err := strconv.Atoi(raw); err == nil {
			cfg.DBMaxIdleConns = n
		}
	case "DB_MAX_OPEN_CONNS":
		if n, err := strconv.Atoi(raw); err == nil {
			cfg.DBMaxOpenConns = n
		}
	case "DB_KEEP_ALIVE_INTERVAL":
		if d, err := time.ParseDuration(raw); err == nil {
			cfg.DBKeepAliveInterval = d
		} else if n, err := strconv.Atoi(raw); err == nil {
			cfg.DBKeepAliveInterval = time.Duration(n) * time.Second
		}
	case "REMINDER_ENABLED":
		cfg.ReminderEnabled = strings.EqualFold(strings.TrimSpace(raw), "true")
	case "REMINDER_WINDOWS_HOURS":
		cfg.ReminderWindowsHours = parseIntCSV(raw, []int{24, 1})
	}
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	}
	return false
}
