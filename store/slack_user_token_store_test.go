package store

import (
	"context"
	"strings"
	"testing"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
)

func TestSaveGetSlackUserToken_RoundTrip(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	email := "slackuser@example.com"
	tok := SlackUserToken{Token: "xoxp-test-token", SlackUserID: "U123", Scopes: "channels:read"}

	if err := SaveSlackUserToken(t.Context(), email, tok); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	got, ok, err := GetSlackUserToken(t.Context(), email)
	if err != nil {
		t.Fatalf("GetSlackUserToken failed: %v", err)
	}
	if !ok {
		t.Fatal("expected token to be found")
	}
	if got != tok {
		t.Errorf("got %+v, want %+v", got, tok)
	}
}

// Why: DB column must be encrypted when TOKEN_ENC_KEY is set — mirrors the Gmail
// token crypto test (store/scan_store_token_test.go).
func TestSaveSlackUserToken_EncryptsColumn(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	origKey := tokenEncKey
	tokenEncKey = make([]byte, 32)
	defer func() { tokenEncKey = origKey }()

	email := "enc-slack@example.com"
	tok := SlackUserToken{Token: "xoxp-secret", SlackUserID: "U456", Scopes: "chat:write"}
	if err := SaveSlackUserToken(t.Context(), email, tok); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	row, err := db.New(GetDB()).GetSlackUserToken(t.Context(), email)
	if err != nil {
		t.Fatalf("failed to read stored token: %v", err)
	}
	if !strings.HasPrefix(row.TokenEnc, encMagic) {
		t.Fatalf("token must be stored encrypted, got %q", row.TokenEnc[:10])
	}
}

func TestGetSlackUserToken_Unknown(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	got, ok, err := GetSlackUserToken(t.Context(), "nobody@example.com")
	if err != nil {
		t.Fatalf("expected no error for unknown email, got %v", err)
	}
	if ok {
		t.Error("expected ok=false for unknown email")
	}
	if got != (SlackUserToken{}) {
		t.Errorf("expected zero value, got %+v", got)
	}
}

func TestDeleteSlackUserToken(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	email := "delete-slack@example.com"
	tok := SlackUserToken{Token: "xoxp-del", SlackUserID: "U789"}

	if err := SaveSlackUserToken(t.Context(), email, tok); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}
	if !HasSlackUserToken(email) {
		t.Fatal("token should exist after save in cache")
	}

	if err := DeleteSlackUserToken(t.Context(), email); err != nil {
		t.Fatalf("failed to delete token: %v", err)
	}
	if HasSlackUserToken(email) {
		t.Error("token should not exist after delete in cache")
	}

	if _, ok, err := GetSlackUserToken(t.Context(), email); err != nil || ok {
		t.Errorf("expected absent after delete, got ok=%v err=%v", ok, err)
	}
}

func TestHasSlackUserToken_CacheOnly(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	if HasSlackUserToken("never-saved@example.com") {
		t.Error("expected false for never-cached email")
	}
}

// Why: rows persist encrypted (encv1:); the startup loader must decrypt into the cache
// or every Slack-as-user scan would break after a restart (same class of bug as the
// 2026-07-23 Gmail token incident).
func TestLoadMetadataDecryptsSlackUserTokens(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	origKey := tokenEncKey
	tokenEncKey = make([]byte, 32)
	defer func() { tokenEncKey = origKey }()

	email := "enc-loader@example.com"
	tok := SlackUserToken{Token: "xoxp-loader", SlackUserID: "U999", Scopes: "channels:read"}
	if err := SaveSlackUserToken(t.Context(), email, tok); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// Simulate a process restart: wipe the in-memory cache, then reload from DB.
	metadataMu.Lock()
	slackUserTokenCache = make(map[string]SlackUserToken)
	metadataMu.Unlock()
	if err := LoadMetadata(context.Background()); err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}

	got, ok, err := GetSlackUserToken(t.Context(), email)
	if err != nil {
		t.Fatalf("GetSlackUserToken failed: %v", err)
	}
	if !ok {
		t.Fatal("expected token to be found after reload")
	}
	if got != tok {
		t.Errorf("cache must hold decrypted token after reload; got %+v, want %+v", got, tok)
	}
}

func TestCreateSlackUserTokensTable_Migration(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	var name string
	err = GetDB().QueryRowContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name='slack_user_tokens'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("slack_user_tokens table should exist after migration: %v", err)
	}
	if name != "slack_user_tokens" {
		t.Errorf("got table name %q", name)
	}
}
