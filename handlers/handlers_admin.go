package handlers

import (
	"errors"
	"message-consolidator/auth"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// adminSettingDTO is what the admin UI consumes per setting.
// Why: `Value` is masked for secrets so the JSON response never leaks plaintext, but `HasValue`
// still tells the UI whether a DB row exists (for the "row vs .env fallback" indicator).
type adminSettingDTO struct {
	Key             string   `json:"key"`
	Label           string   `json:"label"`
	Category        string   `json:"category"`
	Type            string   `json:"type"`
	Secret          bool     `json:"secret"`
	RestartRequired bool     `json:"restart_required"`
	EnumValues      []string `json:"enum_values,omitempty"`
	Value           string   `json:"value"`
	HasValue        bool     `json:"has_value"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	UpdatedBy       string   `json:"updated_by,omitempty"`
}

const maskedSecretValue = "••••••••"

// HandleListAdminSettings returns every registry-defined setting with its current DB-stored value.
// Secrets are masked before serialization.
func (a *API) HandleListAdminSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := store.LoadAllSettings(r.Context())
	if err != nil {
		handleAPIError(w, r, err, "[ADMIN] LoadAllSettings", "Failed to load settings")
		return
	}
	stored := make(map[string]struct {
		value, by string
		at        time.Time
	}, len(rows))
	for _, row := range rows {
		entry := stored[row.Key]
		entry.value = row.Value
		entry.by = row.UpdatedBy
		if row.UpdatedAt.Valid {
			entry.at = row.UpdatedAt.Time
		}
		stored[row.Key] = entry
	}

	out := make([]adminSettingDTO, 0, len(config.Registry))
	for _, def := range config.Registry {
		s, ok := stored[def.Key]
		dto := adminSettingDTO{
			Key:             def.Key,
			Label:           def.Label,
			Category:        def.Category,
			Type:            string(def.Type),
			Secret:          def.Secret,
			RestartRequired: def.RestartRequired,
			EnumValues:      def.EnumValues,
			HasValue:        ok && s.value != "",
		}
		if dto.HasValue {
			if def.Secret {
				dto.Value = maskedSecretValue
			} else {
				dto.Value = s.value
			}
			dto.UpdatedBy = s.by
			if !s.at.IsZero() {
				dto.UpdatedAt = s.at.UTC().Format(time.RFC3339)
			}
		}
		out = append(out, dto)
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleUpdateAdminSetting persists a single setting (PUT /api/admin/settings/{key}).
// Empty body value deletes the row, restoring .env fallback. Hot-reloadable changes are applied
// immediately; restart-required changes return `restart_required: true` so the UI can warn.
func (a *API) HandleUpdateAdminSetting(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	def := config.FindDef(key)
	if def == nil {
		respondError(w, http.StatusNotFound, "Unknown setting key")
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if !bindJSON(w, r, &req) {
		return
	}
	value := strings.TrimSpace(req.Value)
	if err := config.ValidateSetting(def, value); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	updatedBy := auth.GetUserEmail(r)
	if value == "" {
		if err := store.DeleteSetting(r.Context(), key); err != nil {
			handleAPIError(w, r, err, "[ADMIN] DeleteSetting", "Failed to clear setting")
			return
		}
	} else {
		if err := store.UpsertSetting(r.Context(), key, value, updatedBy); err != nil {
			handleAPIError(w, r, err, "[ADMIN] UpsertSetting", "Failed to save setting")
			return
		}
	}

	applied := a.applyHotReload(def, value)
	logger.Infof("[ADMIN] %s updated %s (hot=%v, restart_required=%v)", updatedBy, key, applied, def.RestartRequired)

	respondJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"applied":          applied,
		"restart_required": def.RestartRequired,
	})
}

// applyHotReload mutates live process state for runtime-changeable settings.
// Returns true when the change took effect immediately, false when only persisted (restart needed).
// Why: explicit switch keeps the side-effect surface auditable; unknown keys default to false.
func (a *API) applyHotReload(def *config.SettingDef, value string) bool {
	if def.RestartRequired {
		return false
	}
	switch def.Key {
	case "LOG_LEVEL":
		level := value
		if level == "" {
			level = "INFO"
		}
		logger.SetLevel(level)
		a.Config.LogLevel = level
		return true
	case "ARCHIVE_DAYS":
		n, err := strconv.Atoi(value)
		if err != nil {
			return false
		}
		store.SetAutoArchiveDays(n)
		a.Config.AutoArchiveDays = n
		return true
	case "AUTH_DISABLED":
		flag := strings.EqualFold(value, "true") || value == "1"
		auth.AuthDisabled = flag
		a.Config.AuthDisabled = flag
		return true
	case "DEFAULT_USER_EMAIL":
		// Read live from os.Getenv in auth.GetUserEmail; persist into the cfg so future readers stay in sync.
		return true
	case "GEMINI_ANALYSIS_MODEL":
		a.Config.GeminiAnalysisModel = value
		return true
	case "GEMINI_TRANSLATION_MODEL":
		a.Config.GeminiTranslationModel = value
		return true
	case "COMPANY_DOMAINS":
		a.Config.CompanyDomains = splitCSVForReload(value)
		return true
	case "GMAIL_SKIP_SENDERS":
		a.Config.GmailSkipSenders = value
		return true
	case "MESSAGE_BATCH_WINDOW":
		d, err := time.ParseDuration(value)
		if err != nil {
			return false
		}
		a.Config.MessageBatchWindow = d
		return true
	case "DB_KEEP_ALIVE_INTERVAL":
		// Why: changing this only affects subsequent ticker creation; the running loop keeps its old interval.
		// Persist into cfg so a restart reads the new value, but report applied=false to nudge restart.
		return false
	}
	return false
}

func splitCSVForReload(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// adminUserDTO describes an administrator entry for the UI.
type adminUserDTO struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsSuper bool   `json:"is_super"`
}

// HandleListAdmins returns the super admin plus every user with users.is_admin=1.
func (a *API) HandleListAdmins(w http.ResponseWriter, r *http.Request) {
	users, err := store.ListAdmins(r.Context())
	if err != nil {
		handleAPIError(w, r, err, "[ADMIN] ListAdmins", "Failed to list admins")
		return
	}
	out := make([]adminUserDTO, 0, len(users))
	for _, u := range users {
		out = append(out, adminUserDTO{
			Email:   u.Email,
			Name:    u.Name,
			IsSuper: store.IsSuperAdmin(u.Email),
		})
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleAddAdmin grants admin to an existing user by email.
func (a *API) HandleAddAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !bindJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		respondError(w, http.StatusBadRequest, "email is required")
		return
	}
	if store.IsSuperAdmin(email) {
		respondError(w, http.StatusBadRequest, "super admin is already permanent")
		return
	}
	// Why: ensure the row exists so SetUserAdmin's UPDATE actually flips a flag.
	if _, err := store.GetOrCreateUser(r.Context(), email, "", ""); err != nil {
		handleAPIError(w, r, err, "[ADMIN] GetOrCreateUser", "Failed to ensure user")
		return
	}
	if err := store.SetUserAdmin(r.Context(), email, true); err != nil {
		handleAPIError(w, r, err, "[ADMIN] SetUserAdmin grant", "Failed to grant admin")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleRemoveAdmin revokes admin from a delegated admin. The super admin is always protected.
func (a *API) HandleRemoveAdmin(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(mux.Vars(r)["email"]))
	if email == "" {
		respondError(w, http.StatusBadRequest, "email is required")
		return
	}
	if err := store.SetUserAdmin(r.Context(), email, false); err != nil {
		if errors.Is(err, store.ErrSuperAdminImmutable) {
			respondError(w, http.StatusBadRequest, "super admin role cannot be revoked")
			return
		}
		handleAPIError(w, r, err, "[ADMIN] SetUserAdmin revoke", "Failed to revoke admin")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
