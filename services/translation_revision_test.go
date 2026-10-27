package services

import (
	"message-consolidator/store"
	"testing"
)

func TestBatchTranslationKeyIncludesSourceAndTenant(t *testing.T) {
	original := []store.TranslateRequest{{ID: 1, Text: `{"t":"A","s":["first"]}`}}
	changed := []store.TranslateRequest{{ID: 1, Text: `{"t":"A","s":["second"]}`}}
	key := batchTranslationKey("alice@example.com", original, "ko")
	if key == batchTranslationKey("alice@example.com", changed, "ko") {
		t.Error("different source texts share a flight")
	}
	if key == batchTranslationKey("bob@example.com", original, "ko") {
		t.Error("different tenants share a flight")
	}
	if key == batchTranslationKey("alice@example.com", original, "en") {
		t.Error("different languages share a flight")
	}
	if key != batchTranslationKey("alice@example.com", original, "ko") {
		t.Error("identical requests have different keys")
	}
}
