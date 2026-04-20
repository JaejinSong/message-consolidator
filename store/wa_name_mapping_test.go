package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestWhatsAppNameMapping(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenantEmail := "user@example.com"
	waNumber := "821012345678"
	waPushName := "Hong Gil-dong"

	t.Run("Save New PushName", func(t *testing.T) {
		err := SaveWhatsAppContact(context.Background(), tenantEmail, waNumber, waPushName)
		if err != nil {
			t.Fatalf("SaveWhatsAppContact failed: %v", err)
		}

		resolvedName := GetNameByWhatsAppNumber(tenantEmail, waNumber)
		if resolvedName != waPushName {
			t.Errorf("Expected name %s, got %s", waPushName, resolvedName)
		}
	})

	t.Run("Prevent Overwriting with Number", func(t *testing.T) {
		// When a message comes without a PushName (or PushName is the number itself)
		err := SaveWhatsAppContact(context.Background(), tenantEmail, waNumber, waNumber)
		if err != nil {
			t.Fatalf("SaveWhatsAppContact failed: %v", err)
		}

		// It should still return the previously saved PushName
		resolvedName := GetNameByWhatsAppNumber(tenantEmail, waNumber)
		if resolvedName != waPushName {
			t.Errorf("Expected name %s (preserved), but it was overwritten by %s", waPushName, resolvedName)
		}
	})

	t.Run("Update PushName", func(t *testing.T) {
		newPushName := "Gildong Hong"
		err := SaveWhatsAppContact(context.Background(), tenantEmail, waNumber, newPushName)
		if err != nil {
			t.Fatalf("SaveWhatsAppContact failed: %v", err)
		}

		resolvedName := GetNameByWhatsAppNumber(tenantEmail, waNumber)
		if resolvedName != newPushName {
			t.Errorf("Expected updated name %s, got %s", newPushName, resolvedName)
		}
	})
}
