package store

import (
	"strings"
	"testing"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
)

// Why: SaveGmailToken caches plaintext but persists ciphertext; the startup loader
// must decrypt or every Gmail scan breaks after a restart (2026-07-23 incident).
func TestLoadMetadataDecryptsGmailTokens(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	origKey := tokenEncKey
	tokenEncKey = make([]byte, 32)
	defer func() { tokenEncKey = origKey }()

	email := "enc@example.com"
	plain := `{"access_token":"ya29.x","refresh_token":"r","token_type":"Bearer"}`
	if err := SaveGmailToken(t.Context(), email, plain); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	stored, err := db.New(GetDB()).GetGmailToken(t.Context(), email)
	if err != nil {
		t.Fatalf("failed to read stored token: %v", err)
	}
	if !strings.HasPrefix(stored, encMagic) {
		t.Fatalf("precondition failed: token must be stored encrypted, got %q", stored[:10])
	}

	// Simulate a process restart: wipe the in-memory cache, then reload from DB.
	metadataMu.Lock()
	tokenCache = make(map[string]string)
	metadataMu.Unlock()
	if err := LoadMetadata(); err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}

	got, err := GetGmailToken(t.Context(), email)
	if err != nil {
		t.Fatalf("GetGmailToken failed: %v", err)
	}
	if got != plain {
		t.Errorf("cache must hold decrypted token after reload; got %q", got)
	}
}
