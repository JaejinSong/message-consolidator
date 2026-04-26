package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SettingType discriminates how a string-stored value is parsed and validated for a setting.
type SettingType string

const (
	TypeString   SettingType = "string"
	TypeInt      SettingType = "int"
	TypeBool     SettingType = "bool"
	TypeDuration SettingType = "duration"
	TypeCSV      SettingType = "csv"
	TypeEnum     SettingType = "enum"
)

// SettingDef declares a single env-backed setting that can be exposed in the admin UI.
// `Validate` returns nil for accepted values; nil means "any non-empty string is OK".
// Why: kept as data so the admin handler can iterate without if-chains and so registry-less keys
// are inherently rejected (whitelist gate).
type SettingDef struct {
	Key             string
	Label           string
	Category        string
	Type            SettingType
	Secret          bool
	RestartRequired bool
	DefaultValue    string
	EnumValues      []string
	Validate        func(string) error
}

// Registry is the canonical list of admin-exposable env settings.
// Categories: auth | ai | channels | db | ops
// Why: defining `RestartRequired` here is the source of truth — handlers consult it to decide
// whether to also call applyHotReload, and the UI badges accordingly.
var Registry = []SettingDef{
	// --- ops ---
	{Key: "LOG_LEVEL", Label: "Log Level", Category: "ops", Type: TypeEnum, EnumValues: []string{"DEBUG", "INFO", "WARN", "ERROR"}, DefaultValue: "INFO", Validate: enumValidator("DEBUG", "INFO", "WARN", "ERROR")},
	{Key: "AUTH_DISABLED", Label: "Disable Authentication (dev only)", Category: "auth", Type: TypeBool, DefaultValue: "false", Validate: boolValidator},
	{Key: "DEFAULT_USER_EMAIL", Label: "Default User Email (when AUTH_DISABLED)", Category: "auth", Type: TypeString},
	{Key: "ENV", Label: "Environment Marker", Category: "ops", Type: TypeString, RestartRequired: true},

	// --- AI ---
	{Key: "GEMINI_API_KEY", Label: "Gemini API Key", Category: "ai", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "GEMINI_ANALYSIS_MODEL", Label: "Gemini Analysis Model", Category: "ai", Type: TypeString, DefaultValue: "gemini-3-flash-preview"},
	{Key: "GEMINI_TRANSLATION_MODEL", Label: "Gemini Translation Model", Category: "ai", Type: TypeString, DefaultValue: "gemini-3.1-flash-lite"},

	// --- channels ---
	{Key: "SLACK_TOKEN", Label: "Slack Bot Token", Category: "channels", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "GOOGLE_CLIENT_ID", Label: "Google OAuth Client ID", Category: "channels", Type: TypeString, RestartRequired: true},
	{Key: "GOOGLE_CLIENT_SECRET", Label: "Google OAuth Client Secret", Category: "channels", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "AUTH_SECRET", Label: "Session Auth Secret", Category: "auth", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "APP_BASE_URL", Label: "Application Base URL (OAuth redirect root)", Category: "channels", Type: TypeString, RestartRequired: true},
	{Key: "NOTION_TOKEN", Label: "Notion Integration Token", Category: "channels", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "NOTION_REPORT_PAGE_ID", Label: "Notion Report Parent Page ID", Category: "channels", Type: TypeString, RestartRequired: true},
	{Key: "TELEGRAM_APP_ID", Label: "Telegram App ID", Category: "channels", Type: TypeInt, RestartRequired: true, Validate: intValidator},
	{Key: "TELEGRAM_APP_HASH", Label: "Telegram App Hash", Category: "channels", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "GMAIL_SKIP_SENDERS", Label: "Gmail Skip Senders (CSV)", Category: "channels", Type: TypeCSV},
	{Key: "COMPANY_DOMAINS", Label: "Company Domains (CSV)", Category: "channels", Type: TypeCSV},

	// --- db ---
	{Key: "TURSO_DATABASE_URL", Label: "Turso Database URL", Category: "db", Type: TypeString, RestartRequired: true},
	{Key: "TURSO_AUTH_TOKEN", Label: "Turso Auth Token", Category: "db", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "TURSO_SYNC_URL", Label: "Turso Sync URL", Category: "db", Type: TypeString, RestartRequired: true},
	{Key: "TURSO_SYNC_INTERVAL", Label: "Turso Sync Interval", Category: "db", Type: TypeString, RestartRequired: true},
	{Key: "DB_MAX_IDLE_CONNS", Label: "DB Max Idle Conns", Category: "db", Type: TypeInt, DefaultValue: "1", RestartRequired: true, Validate: intValidator},
	{Key: "DB_MAX_OPEN_CONNS", Label: "DB Max Open Conns", Category: "db", Type: TypeInt, DefaultValue: "25", RestartRequired: true, Validate: intValidator},
	{Key: "DB_KEEP_ALIVE_INTERVAL", Label: "DB Keep-Alive Interval", Category: "db", Type: TypeDuration, DefaultValue: "7s", Validate: durationOrSecondsValidator},

	// --- ops (advanced) ---
	{Key: "ARCHIVE_DAYS", Label: "Auto-Archive Days", Category: "ops", Type: TypeInt, DefaultValue: "7", Validate: intValidator},
	{Key: "MESSAGE_BATCH_WINDOW", Label: "Message Batch Window", Category: "ops", Type: TypeDuration, DefaultValue: "5m", Validate: durationValidator},
	{Key: "INTERNAL_SCAN_SECRET", Label: "Internal Scan Secret", Category: "ops", Type: TypeString, Secret: true, RestartRequired: true},
	{Key: "REMINDER_ENABLED", Label: "Deadline Reminder Enabled", Category: "ops", Type: TypeBool, DefaultValue: "false", Validate: boolValidator},
	{Key: "REMINDER_WINDOWS_HOURS", Label: "Reminder Windows Hours (CSV)", Category: "ops", Type: TypeString, DefaultValue: "24,1"},
}

// FindDef returns the registry entry for `key`, or nil if the key is not exposed to the admin UI.
func FindDef(key string) *SettingDef {
	for i := range Registry {
		if Registry[i].Key == key {
			return &Registry[i]
		}
	}
	return nil
}

// IsRuntimeReloadable reports whether changes to `key` apply without a process restart.
func IsRuntimeReloadable(key string) bool {
	def := FindDef(key)
	if def == nil {
		return false
	}
	return !def.RestartRequired
}

// ValidateSetting checks `value` against the registered validator (if any) plus type-level rules.
// Empty string is always accepted (interpreted as "delete row → fall back to .env").
func ValidateSetting(def *SettingDef, value string) error {
	if def == nil {
		return fmt.Errorf("unknown setting key")
	}
	if value == "" {
		return nil
	}
	if def.Validate != nil {
		return def.Validate(value)
	}
	return nil
}

func boolValidator(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "false", "1", "0", "yes", "no":
		return nil
	}
	return fmt.Errorf("expected boolean (true/false)")
}

func intValidator(s string) error {
	if _, err := strconv.Atoi(strings.TrimSpace(s)); err != nil {
		return fmt.Errorf("expected integer")
	}
	return nil
}

func durationValidator(s string) error {
	if _, err := time.ParseDuration(strings.TrimSpace(s)); err != nil {
		return fmt.Errorf("expected duration (e.g. 5m, 30s)")
	}
	return nil
}

// durationOrSecondsValidator mirrors envDurationOrSeconds: "8s" or bare "8" are both valid.
func durationOrSecondsValidator(s string) error {
	v := strings.TrimSpace(s)
	if _, err := time.ParseDuration(v); err == nil {
		return nil
	}
	if _, err := strconv.Atoi(v); err == nil {
		return nil
	}
	return fmt.Errorf("expected duration or seconds (e.g. 7s, 7)")
}

func enumValidator(allowed ...string) func(string) error {
	return func(s string) error {
		v := strings.TrimSpace(s)
		for _, a := range allowed {
			if strings.EqualFold(v, a) {
				return nil
			}
		}
		return fmt.Errorf("expected one of: %s", strings.Join(allowed, ", "))
	}
}
