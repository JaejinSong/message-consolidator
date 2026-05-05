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
// Why: dispatch table (no reflection) keeps the override surface auditable, lint-friendly,
// and bounded in cyclomatic complexity regardless of key count.
func applyOverlay(cfg *Config, v map[string]string) {
	for key, raw := range v {
		if FindDef(key) == nil {
			continue
		}
		if setter, ok := overlaySetters[key]; ok {
			setter(cfg, raw)
		}
	}
}

type fieldSetter func(*Config, string)

// Why: each entry maps an admin-managed key to a typed setter. Lookup keeps assignField at O(1)
// branches and confines per-key parsing to its closure — extending the registry adds one entry.
var overlaySetters = map[string]fieldSetter{
	"SLACK_TOKEN":                  func(c *Config, r string) { c.SlackToken = r },
	"GEMINI_API_KEY":               func(c *Config, r string) { c.GeminiAPIKey = r },
	"GOOGLE_CLIENT_ID":             func(c *Config, r string) { c.GoogleClientID = r },
	"GOOGLE_CLIENT_SECRET":         func(c *Config, r string) { c.GoogleClientSecret = r },
	"AUTH_SECRET":                  func(c *Config, r string) { c.AuthSecret = r },
	"AUTH_DISABLED":                func(c *Config, r string) { c.AuthDisabled = parseBool(r) },
	"APP_BASE_URL":                 func(c *Config, r string) { c.AppBaseURL = r },
	"TURSO_DATABASE_URL":           func(c *Config, r string) { c.TursoURL = r },
	"TURSO_AUTH_TOKEN":             func(c *Config, r string) { c.TursoToken = r },
	"TURSO_SYNC_URL":               func(c *Config, r string) { c.TursoSyncURL = r },
	"TURSO_SYNC_INTERVAL":          func(c *Config, r string) { c.TursoSyncInterval = r },
	"GEMINI_ANALYSIS_MODEL":        func(c *Config, r string) { c.GeminiAnalysisModel = r },
	"GEMINI_TRANSLATION_MODEL":     func(c *Config, r string) { c.GeminiTranslationModel = r },
	"LOG_LEVEL":                    func(c *Config, r string) { c.LogLevel = r },
	"GMAIL_SKIP_SENDERS":           func(c *Config, r string) { c.GmailSkipSenders = r },
	"COMPANY_DOMAINS":              func(c *Config, r string) { c.CompanyDomains = splitCSV(r) },
	"ARCHIVE_DAYS":                 func(c *Config, r string) { setIntIfValid(&c.AutoArchiveDays, r) },
	"NOTION_TOKEN":                 func(c *Config, r string) { c.NotionToken = r },
	"NOTION_REPORT_PAGE_ID":        func(c *Config, r string) { c.NotionReportPageID = r },
	"TELEGRAM_APP_ID":              func(c *Config, r string) { setIntIfValid(&c.TelegramAppID, r) },
	"TELEGRAM_APP_HASH":            func(c *Config, r string) { c.TelegramAppHash = r },
	"INTERNAL_SCAN_SECRET":         func(c *Config, r string) { c.InternalScanSecret = r },
	"MESSAGE_BATCH_WINDOW":         func(c *Config, r string) { setDurationIfValid(&c.MessageBatchWindow, r) },
	"DB_MAX_IDLE_CONNS":            func(c *Config, r string) { setIntIfValid(&c.DBMaxIdleConns, r) },
	"DB_MAX_OPEN_CONNS":            func(c *Config, r string) { setIntIfValid(&c.DBMaxOpenConns, r) },
	"REMINDER_ENABLED":             func(c *Config, r string) { c.ReminderEnabled = parseBool(r) },
	"REMINDER_WINDOWS_HOURS":       func(c *Config, r string) { c.ReminderWindowsHours = parseIntCSV(r, []int{24, 1}) },
	"STALE_THRESHOLD_WORKING_DAYS": func(c *Config, r string) { setIntIfValid(&c.StaleThresholdWorkingDays, r) },
	"DAILY_DIGEST_ENABLED":         func(c *Config, r string) { c.DailyDigestEnabled = parseBool(r) },
	"DAILY_DIGEST_RECIPIENT_EMAIL": func(c *Config, r string) { c.DailyDigestRecipientEmails = splitCSV(r) },
	"DAILY_DIGEST_HOUR":            func(c *Config, r string) { setIntIfValid(&c.DailyDigestHour, r) },
	"DAILY_DIGEST_TIMEZONE":        func(c *Config, r string) { c.DailyDigestTimezone = r },
	"DAILY_DIGEST_LANGUAGE":        func(c *Config, r string) { c.DailyDigestLanguage = r },
}

func setIntIfValid(target *int, raw string) {
	if n, err := strconv.Atoi(raw); err == nil {
		*target = n
	}
}

func setDurationIfValid(target *time.Duration, raw string) {
	if d, err := time.ParseDuration(raw); err == nil {
		*target = d
	}
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	}
	return false
}
