package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"message-consolidator/auth"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildSlackAliases(t *testing.T) {
	cases := []struct {
		realName    string
		displayName string
		expected    []string
	}{
		{"Jaejin Song", "JJ", []string{"Jaejin Song", "JJ"}},
		{"Jaejin Song", "Jaejin Song", []string{"Jaejin Song"}},
		{"Jaejin Song", "", []string{"Jaejin Song"}},
		{"", "JJ", []string{"JJ"}},
		{"", "", []string{}},
	}
	for _, tc := range cases {
		got := buildSlackAliases(tc.realName, tc.displayName)
		if len(got) != len(tc.expected) {
			t.Errorf("buildSlackAliases(%q, %q) len=%d, want %d", tc.realName, tc.displayName, len(got), len(tc.expected))
			continue
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Errorf("buildSlackAliases(%q, %q)[%d] = %q, want %q", tc.realName, tc.displayName, i, got[i], tc.expected[i])
			}
		}
	}
}

func TestDetermineCanonicalID(t *testing.T) {
	cases := []struct {
		displayName string
		aliases     string
		canonicalID string
		want        string
	}{
		{"Foo <foo@bar.com>", "", "", "foo@bar.com"},
		{"Foo", "alias@x.io", "", "alias@x.io"},
		{"Foo", "", "canon@y.io", "canon@y.io"},
		{"Foo Bar", "", "", "foobar"},
		{"", "", "", ""},
		{" UPPER@X.com ", "", "", "upper@x.com"},
	}
	for _, tc := range cases {
		got := determineCanonicalID(tc.displayName, tc.aliases, tc.canonicalID)
		if got != tc.want {
			t.Errorf("determineCanonicalID(%q, %q, %q) = %q, want %q", tc.displayName, tc.aliases, tc.canonicalID, got, tc.want)
		}
	}
}

func TestHandleMappingError(t *testing.T) {
	t.Run("UNIQUE constraint returns 409", func(t *testing.T) {
		rr := httptest.NewRecorder()
		err := fmt.Errorf("UNIQUE constraint failed: contacts.tenant_email, contacts.canonical_id")
		handleMappingError(rr, err, "user@example.com", "canonical@example.com")
		if rr.Code != http.StatusConflict {
			t.Errorf("expected 409, got %d", rr.Code)
		}
		var body map[string]string
		if jsonErr := json.Unmarshal(rr.Body.Bytes(), &body); jsonErr != nil {
			t.Fatalf("body not JSON: %v", jsonErr)
		}
		if body["error"] != "Mapping already exists for this identity" {
			t.Errorf("unexpected error message: %q", body["error"])
		}
	})

	t.Run("generic error returns 500", func(t *testing.T) {
		rr := httptest.NewRecorder()
		err := errors.New("disk full")
		handleMappingError(rr, err, "user@example.com", "canonical@example.com")
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rr.Code)
		}
		var body map[string]string
		if jsonErr := json.Unmarshal(rr.Body.Bytes(), &body); jsonErr != nil {
			t.Fatalf("body not JSON: %v", jsonErr)
		}
		if body["error"] != "Internal Server Error" {
			t.Errorf("unexpected error message: %q", body["error"])
		}
	})
}

func TestGatherTokenUsageStats(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{SlackToken: ""}}
	email := "tokentest@example.com"

	got := api.gatherTokenUsageStats(context.Background(), email)
	if got.TodayPrompt != 0 || got.TodayCompletion != 0 || got.TodayFiltered != 0 ||
		got.TodayTotal != 0 || got.MonthlyPrompt != 0 || got.MonthlyCompletion != 0 ||
		got.MonthlyFiltered != 0 || got.MonthlyTotal != 0 {
		t.Errorf("expected all zero counts for new user, got %+v", got)
	}
	if got.TodayCost != 0.0 || got.MonthlyCost != 0.0 {
		t.Errorf("expected zero costs, got today=%f monthly=%f", got.TodayCost, got.MonthlyCost)
	}
	if got.Model != "Gemini 3 Flash" {
		t.Errorf("expected Model=Gemini 3 Flash, got %q", got.Model)
	}
}

func TestCostsByProvider(t *testing.T) {
	near := func(a, b float64) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 1e-9
	}
	// 1M-token rows so cost == rate (cost = tokens / 1e6 * rate).
	models := []store.ModelTokenUsage{
		{Model: "deepseek-chat", Prompt: 1_000_000, Completion: 1_000_000},          // 0.14 in + 0.28 out
		{Model: "deepseek-v4-pro", Completion: 1_000_000},                           // 1.98 out (off-peak)
		{Model: "deepseek-v4-flash", Peak: true, Completion: 1_000_000},             // 0.66 x 2 = 1.32 out
		{Model: "gemini-3-flash-preview", Prompt: 1_000_000, Completion: 1_000_000}, // 0.50 in + 3.00 out
	}
	got := costsByProvider(models)

	if len(got) != 2 {
		t.Fatalf("expected 2 providers, got %d: %+v", len(got), got)
	}
	if got[0].Provider != "DeepSeek" || got[1].Provider != "Gemini" {
		t.Fatalf("expected fixed order [DeepSeek, Gemini], got [%s, %s]", got[0].Provider, got[1].Provider)
	}

	ds := got[0]
	if ds.Prompt != 1_000_000 || ds.Completion != 3_000_000 {
		t.Errorf("DeepSeek tokens: prompt=%d completion=%d, want 1000000/3000000", ds.Prompt, ds.Completion)
	}
	if wantDS := 0.14 + 0.28 + 1.98 + 1.32; !near(ds.Cost, wantDS) {
		t.Errorf("DeepSeek cost = %f, want %f", ds.Cost, wantDS)
	}

	gm := got[1]
	if wantGM := 0.50 + 3.00; !near(gm.Cost, wantGM) {
		t.Errorf("Gemini cost = %f, want %f", gm.Cost, wantGM)
	}
}

func TestCostsByProviderEmpty(t *testing.T) {
	if got := costsByProvider(nil); len(got) != 0 {
		t.Errorf("expected no rows for empty usage, got %+v", got)
	}
}

// TestGatherTokenUsageStatsByProvider verifies the end-to-end path: a mixed
// Gemini+DeepSeek month seeded in token_usage flows through GetMonthlyTokenUsageByModel
// and costsByProvider into the response's MonthlyByProvider split, and the per-provider
// costs sum to the aggregate MonthlyCost.
func TestGatherTokenUsageStatsByProvider(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup DB: %v", err)
	}
	defer cleanup()

	near := func(a, b float64) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 1e-9
	}

	email := "byprovider@example.com"
	if _, err := store.GetOrCreateUser(context.Background(), email, "", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// 1M-token rows so each cost component equals its rate.
	if err := store.AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 1_000_000, 1_000_000, 0, 0); err != nil {
		t.Fatalf("seed deepseek: %v", err)
	}
	if err := store.AddTokenUsage(email, "Analyze", "gemini-3-flash-preview", "slack", 0, 1_000_000, 1_000_000, 0, 0); err != nil {
		t.Fatalf("seed gemini: %v", err)
	}

	api := &API{Config: &config.Config{}}
	got := api.gatherTokenUsageStats(context.Background(), email)

	if len(got.MonthlyByProvider) != 2 {
		t.Fatalf("expected 2 providers, got %d: %+v", len(got.MonthlyByProvider), got.MonthlyByProvider)
	}
	ds, gm := got.MonthlyByProvider[0], got.MonthlyByProvider[1]
	if ds.Provider != "DeepSeek" || gm.Provider != "Gemini" {
		t.Fatalf("expected order [DeepSeek, Gemini], got [%s, %s]", ds.Provider, gm.Provider)
	}
	if !near(ds.Cost, 0.14+0.28) {
		t.Errorf("DeepSeek cost = %f, want %f", ds.Cost, 0.14+0.28)
	}
	if !near(gm.Cost, 0.50+3.00) {
		t.Errorf("Gemini cost = %f, want %f", gm.Cost, 0.50+3.00)
	}
	if !near(got.MonthlyCost, ds.Cost+gm.Cost) {
		t.Errorf("MonthlyCost %f != sum of per-provider %f", got.MonthlyCost, ds.Cost+gm.Cost)
	}
}

func TestHandleGetTokenUsage(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "tokenusage@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")

	api := &API{Config: &config.Config{SlackToken: ""}}
	req := NewMockRequest("GET", "/api/token-usage", email)
	rr := httptest.NewRecorder()
	api.HandleGetTokenUsage(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var body map[string]interface{}
	if jsonErr := json.Unmarshal(rr.Body.Bytes(), &body); jsonErr != nil {
		t.Fatalf("body not JSON: %v", jsonErr)
	}
	if model, ok := body["model"]; !ok || model != "Gemini 3 Flash" {
		t.Errorf("expected model=Gemini 3 Flash, got %v", body["model"])
	}
}

func TestHandleGetUserAliases(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "aliases@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")

	api := &API{Config: &config.Config{SlackToken: ""}}
	req := NewMockRequest("GET", "/api/user/aliases", email)
	rr := httptest.NewRecorder()
	api.HandleGetUserAliases(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	// Body must be a JSON array (possibly empty).
	body := strings.TrimSpace(rr.Body.String())
	if len(body) == 0 || (body[0] != '[') {
		t.Errorf("expected JSON array, got %q", body)
	}
}

func TestHandleAddAlias(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "addalias@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/user/aliases/add", bytes.NewBuffer([]byte("{not-json")))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleAddAlias(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"alias": "newalias"})
		r, _ := http.NewRequest("POST", "/api/user/aliases/add", bytes.NewBuffer(body))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleAddAlias(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

func TestHandleDeleteAlias(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "deletealias@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/user/aliases/delete", bytes.NewBuffer([]byte("{bad")))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleDeleteAlias(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"alias": "x"})
		r, _ := http.NewRequest("POST", "/api/user/aliases/delete", bytes.NewBuffer(body))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleDeleteAlias(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

func TestHandleGetTenantAliases(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "tenantaliases@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")

	api := &API{Config: &config.Config{SlackToken: ""}}
	req := NewMockRequest("GET", "/api/contacts/aliases", email)
	rr := httptest.NewRecorder()
	api.HandleGetTenantAliases(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	assertJSONArrayOrNull(t, rr.Body.String())
}

func TestHandleAddTenantAlias(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "addtenalias@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	payload, _ := json.Marshal(map[string]string{
		"canonical_id": "external@client.com",
		"display_name": "External Client",
		"aliases":      "",
		"source":       "user",
	})
	r, _ := http.NewRequest("POST", "/api/contacts/aliases/add", bytes.NewBuffer(payload))
	r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
	rr := httptest.NewRecorder()
	api.HandleAddTenantAlias(rr, r)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleDeleteTenantAlias(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "deltenalias@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	// Add first so delete has something to remove.
	addPayload, _ := json.Marshal(map[string]string{
		"canonical_id": "todelete@client.com",
		"display_name": "To Delete",
		"aliases":      "",
		"source":       "user",
	})
	r1, _ := http.NewRequest("POST", "/api/contacts/aliases/add", bytes.NewBuffer(addPayload))
	r1 = r1.WithContext(context.WithValue(r1.Context(), auth.UserEmailKey, email))
	rr1 := httptest.NewRecorder()
	api.HandleAddTenantAlias(rr1, r1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("add precondition failed: %d %s", rr1.Code, rr1.Body.String())
	}

	delPayload, _ := json.Marshal(map[string]string{"canonical_id": "todelete@client.com"})
	r2, _ := http.NewRequest("POST", "/api/contacts/aliases/delete", bytes.NewBuffer(delPayload))
	r2 = r2.WithContext(context.WithValue(r2.Context(), auth.UserEmailKey, email))
	rr2 := httptest.NewRecorder()
	api.HandleDeleteTenantAlias(rr2, r2)

	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestHandleGetMappings(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "mappings@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")

	api := &API{Config: &config.Config{SlackToken: ""}}
	req := NewMockRequest("GET", "/api/contacts/mappings", email)
	rr := httptest.NewRecorder()
	api.HandleGetMappings(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	assertJSONArrayOrNull(t, rr.Body.String())
}

func TestHandleDeleteMapping(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "delmapping@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/contacts/mapping/delete", bytes.NewBuffer([]byte("{bad")))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleDeleteMapping(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		// Seed a contact to delete.
		_ = store.AddContactMapping(context.Background(), email, "target@example.com", "Target", "", "user")

		payload, _ := json.Marshal(map[string]string{"canonical_id": "target@example.com"})
		r, _ := http.NewRequest("POST", "/api/contacts/mapping/delete", bytes.NewBuffer(payload))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleDeleteMapping(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleSearchContacts(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "search@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	t.Run("Empty query", func(t *testing.T) {
		req := NewMockRequest("GET", "/api/contacts/search", email)
		rr := httptest.NewRecorder()
		api.HandleSearchContacts(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		if strings.TrimSpace(rr.Body.String()) != "[]" {
			t.Errorf("expected [], got %q", rr.Body.String())
		}
	})

	t.Run("With query", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/api/contacts/search?q=foo", nil)
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleSearchContacts(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		assertJSONArrayOrNull(t, rr.Body.String())
	})
}

// Why: store funcs return nil slice on empty result, which json.Marshal renders as "null" not "[]".
func assertJSONArrayOrNull(t *testing.T, body string) {
	t.Helper()
	trimmed := strings.TrimSpace(body)
	if trimmed == "null" || (len(trimmed) > 0 && trimmed[0] == '[') {
		return
	}
	t.Errorf("expected JSON array or null, got %q", trimmed)
}

func TestHandleLinkAccounts(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "link@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/contacts/link", bytes.NewBuffer([]byte("{bad")))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleLinkAccounts(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Self-link rejected", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]int64{"target_id": 5, "master_id": 5})
		r, _ := http.NewRequest("POST", "/api/contacts/link", bytes.NewBuffer(payload))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleLinkAccounts(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
		var body map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["error"] != "Cannot link account to itself" {
			t.Errorf("unexpected error: %q", body["error"])
		}
	})

	// Why: Success path requires seeding two existing contact rows with valid DB IDs,
	// which is non-trivial and couples to store internals. Covered by store-layer tests.
}

func TestHandleUnlinkAccount(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "unlink@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")
	api := &API{Config: &config.Config{SlackToken: ""}}

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/contacts/unlink", bytes.NewBuffer([]byte("{bad")))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleUnlinkAccount(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	// Why: Success path requires seeding a linked contact pair; covered by store-layer linking tests.
}

func TestHandleGetLinks(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "links@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "", "")

	api := &API{Config: &config.Config{SlackToken: ""}}
	req := NewMockRequest("GET", "/api/contacts/links", email)
	rr := httptest.NewRecorder()
	api.HandleGetLinks(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	assertJSONArrayOrNull(t, rr.Body.String())
}

func TestHandleUserInfo(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	api := &API{Config: &config.Config{SlackToken: ""}}

	t.Run("Regular user", func(t *testing.T) {
		email := "regularuser@example.com"
		_, _ = store.GetOrCreateUser(context.Background(), email, "Regular User", "")

		req := NewMockRequest("GET", "/api/user/info", email)
		rr := httptest.NewRecorder()
		api.HandleUserInfo(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		if jsonErr := json.Unmarshal(rr.Body.Bytes(), &body); jsonErr != nil {
			t.Fatalf("body not JSON: %v", jsonErr)
		}
		if body["email"] != email {
			t.Errorf("expected email=%q, got %v", email, body["email"])
		}
		if isSuperAdmin, ok := body["is_super_admin"].(bool); !ok || isSuperAdmin {
			t.Errorf("expected is_super_admin=false, got %v", body["is_super_admin"])
		}
		tokenUsage, ok := body["token_usage"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected token_usage object, got %T", body["token_usage"])
		}
		if tokenUsage["model"] != "Gemini 3 Flash" {
			t.Errorf("expected token_usage.model=Gemini 3 Flash, got %v", tokenUsage["model"])
		}
	})

	t.Run("Super admin", func(t *testing.T) {
		// Why: SuperAdminEmail is a hardcoded constant in store package; use it directly.
		email := store.SuperAdminEmail
		_, _ = store.GetOrCreateUser(context.Background(), email, "Super Admin", "")

		req := NewMockRequest("GET", "/api/user/info", email)
		rr := httptest.NewRecorder()
		api.HandleUserInfo(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var body map[string]interface{}
		if jsonErr := json.Unmarshal(rr.Body.Bytes(), &body); jsonErr != nil {
			t.Fatalf("body not JSON: %v", jsonErr)
		}
		if isSuperAdmin, ok := body["is_super_admin"].(bool); !ok || !isSuperAdmin {
			t.Errorf("expected is_super_admin=true, got %v", body["is_super_admin"])
		}
	})
}

func TestHandleAddMappingConflict(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	api := &API{}
	tenantEmail := "admin@example.com"

	payload := map[string]string{
		"display_name": "Conflict User",
		"canonical_id": "conflict@gmail.com",
		"aliases":      "conflict-alias",
		"source":       "gmail",
	}
	body, _ := json.Marshal(payload)

	// 1. First attempt (Success)
	t.Run("Initial Add", func(t *testing.T) {
		w1 := httptest.NewRecorder()
		r1, _ := http.NewRequest("POST", "/api/contacts/mapping/add", bytes.NewBuffer(body))
		ctx := context.WithValue(r1.Context(), auth.UserEmailKey, tenantEmail)
		r1 = r1.WithContext(ctx)

		api.HandleAddMapping(w1, r1)

		if w1.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w1.Code)
		}
	})

	// 2. Second attempt (Idempotent Add)
	t.Run("Idempotent Add", func(t *testing.T) {
		w2 := httptest.NewRecorder()
		r2, _ := http.NewRequest("POST", "/api/contacts/mapping/add", bytes.NewBuffer(body))
		ctx := context.WithValue(r2.Context(), auth.UserEmailKey, tenantEmail)
		r2 = r2.WithContext(ctx)

		api.HandleAddMapping(w2, r2)

		if w2.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for idempotent add, got %d", w2.Code)
		}
	})
}
