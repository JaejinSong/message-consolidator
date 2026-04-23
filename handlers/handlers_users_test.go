package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"message-consolidator/auth"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsSameSliceSkipsUpdateWhenNamesMatch(t *testing.T) {
	// Regression: GetUserAliasesByEmailFromCache previously returned [email, name],
	// causing isSameSlice to always return false (len mismatch) and triggering a DB write every request.
	// Now GetUserAliasesByEmailFromCache returns only display names, so comparison works directly.
	userAliases := []string{"Jaejin Song"}
	newAliases := []string{"Jaejin Song"}

	if !isSameSlice(userAliases, newAliases) {
		t.Error("expected no update needed when names already match")
	}
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
		"aliases":     "conflict-alias",
		"source":      "gmail",
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
