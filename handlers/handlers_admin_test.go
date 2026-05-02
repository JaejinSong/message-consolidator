package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"message-consolidator/auth"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestSplitCSVForReload(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"  A , B  ,, C ", []string{"a", "b", "c"}},
		{",,,", []string{}},
	}
	for _, tc := range cases {
		got := splitCSVForReload(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitCSVForReload(%q) len=%d, want %d (got=%v)", tc.in, len(got), len(tc.want), got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitCSVForReload(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestApplyHotReload(t *testing.T) {
	t.Run("RestartRequired returns false", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "GEMINI_API_KEY", RestartRequired: true}
		if api.applyHotReload(def, "anything") {
			t.Error("expected false for RestartRequired def")
		}
	})

	t.Run("LOG_LEVEL applies", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "LOG_LEVEL"}
		if !api.applyHotReload(def, "DEBUG") {
			t.Error("expected true")
		}
		if api.Config.LogLevel != "DEBUG" {
			t.Errorf("Config.LogLevel = %q, want DEBUG", api.Config.LogLevel)
		}
	})

	t.Run("LOG_LEVEL empty defaults to INFO", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "LOG_LEVEL"}
		api.applyHotReload(def, "")
		if api.Config.LogLevel != "INFO" {
			t.Errorf("Config.LogLevel = %q, want INFO", api.Config.LogLevel)
		}
	})

	t.Run("ARCHIVE_DAYS valid int", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "ARCHIVE_DAYS"}
		if !api.applyHotReload(def, "14") {
			t.Error("expected true")
		}
		if api.Config.AutoArchiveDays != 14 {
			t.Errorf("AutoArchiveDays = %d, want 14", api.Config.AutoArchiveDays)
		}
	})

	t.Run("ARCHIVE_DAYS invalid int returns false", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "ARCHIVE_DAYS"}
		if api.applyHotReload(def, "abc") {
			t.Error("expected false for non-numeric")
		}
	})

	t.Run("AUTH_DISABLED true", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "AUTH_DISABLED"}
		api.applyHotReload(def, "true")
		if !api.Config.AuthDisabled || !auth.AuthDisabled {
			t.Errorf("AuthDisabled cfg=%v auth=%v", api.Config.AuthDisabled, auth.AuthDisabled)
		}
		api.applyHotReload(def, "false")
		if api.Config.AuthDisabled || auth.AuthDisabled {
			t.Errorf("AuthDisabled should be false; cfg=%v auth=%v", api.Config.AuthDisabled, auth.AuthDisabled)
		}
	})

	t.Run("GEMINI_ANALYSIS_MODEL", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "GEMINI_ANALYSIS_MODEL"}
		if !api.applyHotReload(def, "gemini-x") {
			t.Error("expected true")
		}
		if api.Config.GeminiAnalysisModel != "gemini-x" {
			t.Errorf("GeminiAnalysisModel = %q", api.Config.GeminiAnalysisModel)
		}
	})

	t.Run("COMPANY_DOMAINS", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "COMPANY_DOMAINS"}
		if !api.applyHotReload(def, "Foo.com, Bar.io") {
			t.Error("expected true")
		}
		want := []string{"foo.com", "bar.io"}
		got := api.Config.CompanyDomains
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("CompanyDomains = %v, want %v", got, want)
		}
	})

	t.Run("GMAIL_SKIP_SENDERS", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "GMAIL_SKIP_SENDERS"}
		api.applyHotReload(def, "noreply@x.com")
		if api.Config.GmailSkipSenders != "noreply@x.com" {
			t.Errorf("GmailSkipSenders = %q", api.Config.GmailSkipSenders)
		}
	})

	t.Run("MESSAGE_BATCH_WINDOW valid", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "MESSAGE_BATCH_WINDOW"}
		if !api.applyHotReload(def, "10m") {
			t.Error("expected true")
		}
		if api.Config.MessageBatchWindow != 10*time.Minute {
			t.Errorf("MessageBatchWindow = %v", api.Config.MessageBatchWindow)
		}
	})

	t.Run("MESSAGE_BATCH_WINDOW invalid duration", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "MESSAGE_BATCH_WINDOW"}
		if api.applyHotReload(def, "not-a-duration") {
			t.Error("expected false")
		}
	})

	t.Run("DB_KEEP_ALIVE_INTERVAL persists but reports false", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "DB_KEEP_ALIVE_INTERVAL"}
		if api.applyHotReload(def, "5s") {
			t.Error("expected false (restart nudge)")
		}
	})

	t.Run("DEFAULT_USER_EMAIL", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "DEFAULT_USER_EMAIL"}
		if !api.applyHotReload(def, "x@y.io") {
			t.Error("expected true")
		}
	})

	t.Run("Unknown key returns false", func(t *testing.T) {
		api := &API{Config: &config.Config{}}
		def := &config.SettingDef{Key: "NEVER_DEFINED_XYZ"}
		if api.applyHotReload(def, "v") {
			t.Error("expected false for unknown key")
		}
	})
}

func TestHandleListAdminSettings(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup DB: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{}}

	t.Run("Empty DB returns full registry", func(t *testing.T) {
		req := NewMockRequest("GET", "/api/admin/settings", store.SuperAdminEmail)
		rr := httptest.NewRecorder()
		api.HandleListAdminSettings(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d", rr.Code)
		}
		var out []adminSettingDTO
		if e := json.Unmarshal(rr.Body.Bytes(), &out); e != nil {
			t.Fatalf("unmarshal: %v", e)
		}
		if len(out) != len(config.Registry) {
			t.Errorf("len = %d, want %d", len(out), len(config.Registry))
		}
		for _, dto := range out {
			if dto.HasValue {
				t.Errorf("expected has_value=false for empty DB, got %+v", dto)
			}
		}
	})

	t.Run("Secret value is masked", func(t *testing.T) {
		_ = store.UpsertSetting(context.Background(), "GEMINI_API_KEY", "secret123", store.SuperAdminEmail)
		req := NewMockRequest("GET", "/api/admin/settings", store.SuperAdminEmail)
		rr := httptest.NewRecorder()
		api.HandleListAdminSettings(rr, req)
		var out []adminSettingDTO
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		var found *adminSettingDTO
		for i := range out {
			if out[i].Key == "GEMINI_API_KEY" {
				found = &out[i]
				break
			}
		}
		if found == nil {
			t.Fatal("GEMINI_API_KEY not in response")
		}
		if !found.HasValue {
			t.Error("expected has_value=true")
		}
		if found.Value != maskedSecretValue {
			t.Errorf("expected masked %q, got %q (LEAK!)", maskedSecretValue, found.Value)
		}
	})

	t.Run("Non-secret value passes through", func(t *testing.T) {
		_ = store.UpsertSetting(context.Background(), "ARCHIVE_DAYS", "21", store.SuperAdminEmail)
		req := NewMockRequest("GET", "/api/admin/settings", store.SuperAdminEmail)
		rr := httptest.NewRecorder()
		api.HandleListAdminSettings(rr, req)
		var out []adminSettingDTO
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		for _, dto := range out {
			if dto.Key == "ARCHIVE_DAYS" {
				if dto.Value != "21" {
					t.Errorf("ARCHIVE_DAYS value = %q, want 21", dto.Value)
				}
				return
			}
		}
		t.Error("ARCHIVE_DAYS missing from response")
	})
}

func TestHandleUpdateAdminSetting(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup DB: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{}}

	newReq := func(key, body string) *http.Request {
		r, _ := http.NewRequest("PUT", "/api/admin/settings/"+key, bytes.NewBufferString(body))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, store.SuperAdminEmail))
		r = mux.SetURLVars(r, map[string]string{"key": key})
		return r
	}

	t.Run("Unknown key returns 404", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleUpdateAdminSetting(rr, newReq("NEVER_DEFINED_XYZ", `{"value":"x"}`))
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("Invalid JSON returns 400", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleUpdateAdminSetting(rr, newReq("ARCHIVE_DAYS", "{not-json"))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Validator rejects bad value", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleUpdateAdminSetting(rr, newReq("ARCHIVE_DAYS", `{"value":"abc"}`))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Valid value persists and applies", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleUpdateAdminSetting(rr, newReq("ARCHIVE_DAYS", `{"value":"30"}`))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %v", resp["status"])
		}
		if api.Config.AutoArchiveDays != 30 {
			t.Errorf("Config.AutoArchiveDays = %d, want 30", api.Config.AutoArchiveDays)
		}
	})

	t.Run("Empty value deletes row", func(t *testing.T) {
		_ = store.UpsertSetting(context.Background(), "ARCHIVE_DAYS", "30", store.SuperAdminEmail)
		rr := httptest.NewRecorder()
		api.HandleUpdateAdminSetting(rr, newReq("ARCHIVE_DAYS", `{"value":""}`))
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		rows, _ := store.LoadAllSettings(context.Background())
		for _, row := range rows {
			if row.Key == "ARCHIVE_DAYS" {
				t.Errorf("ARCHIVE_DAYS row not deleted: %+v", row)
			}
		}
	})
}

func TestHandleListAdmins(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup DB: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{}}
	req := NewMockRequest("GET", "/api/admin/admins", store.SuperAdminEmail)
	rr := httptest.NewRecorder()
	api.HandleListAdmins(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var out []adminUserDTO
	if e := json.Unmarshal(rr.Body.Bytes(), &out); e != nil {
		t.Fatalf("unmarshal: %v", e)
	}
	foundSuper := false
	for _, u := range out {
		if u.Email == store.SuperAdminEmail && u.IsSuper {
			foundSuper = true
		}
	}
	if !foundSuper {
		t.Errorf("super admin missing or IsSuper=false: %+v", out)
	}
}

func TestHandleAddAdmin(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup DB: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{}}

	newReq := func(body string) *http.Request {
		r, _ := http.NewRequest("POST", "/api/admin/admins", bytes.NewBufferString(body))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, store.SuperAdminEmail))
		return r
	}

	t.Run("Invalid JSON", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleAddAdmin(rr, newReq("{bad"))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Empty email", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleAddAdmin(rr, newReq(`{"email":"  "}`))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "email is required") {
			t.Errorf("unexpected body: %s", rr.Body.String())
		}
	})

	t.Run("Super admin rejected", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleAddAdmin(rr, newReq(`{"email":"`+store.SuperAdminEmail+`"}`))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "super admin is already permanent") {
			t.Errorf("unexpected body: %s", rr.Body.String())
		}
	})

	t.Run("Success grants admin", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleAddAdmin(rr, newReq(`{"email":"newadmin@example.com"}`))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		admins, _ := store.ListAdmins(context.Background())
		found := false
		for _, u := range admins {
			if u.Email == "newadmin@example.com" && u.IsAdmin {
				found = true
			}
		}
		if !found {
			t.Errorf("newadmin not in ListAdmins: %+v", admins)
		}
	})
}

func TestHandleRemoveAdmin(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup DB: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{}}

	newReq := func(email string) *http.Request {
		r, _ := http.NewRequest("DELETE", "/api/admin/admins/"+email, nil)
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, store.SuperAdminEmail))
		r = mux.SetURLVars(r, map[string]string{"email": email})
		return r
	}

	t.Run("Empty email", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleRemoveAdmin(rr, newReq(""))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Super admin protected", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleRemoveAdmin(rr, newReq(store.SuperAdminEmail))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "super admin role cannot be revoked") {
			t.Errorf("unexpected body: %s", rr.Body.String())
		}
	})

	t.Run("Success revokes", func(t *testing.T) {
		_, _ = store.GetOrCreateUser(context.Background(), "revoke@example.com", "", "")
		_ = store.SetUserAdmin(context.Background(), "revoke@example.com", true)

		rr := httptest.NewRecorder()
		api.HandleRemoveAdmin(rr, newReq("revoke@example.com"))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		admins, _ := store.ListAdmins(context.Background())
		for _, u := range admins {
			if u.Email == "revoke@example.com" && u.IsAdmin {
				t.Errorf("revoke@example.com still admin")
			}
		}
	})
}

func TestReloadGeminiTranslationModel(t *testing.T) {
	api := &API{Config: &config.Config{}}
	if !reloadGeminiTranslationModel(api, "gemini-1.5-pro") {
		t.Error("reloadGeminiTranslationModel should return true")
	}
	if api.Config.GeminiTranslationModel != "gemini-1.5-pro" {
		t.Errorf("GeminiTranslationModel = %q, want gemini-1.5-pro", api.Config.GeminiTranslationModel)
	}
}
