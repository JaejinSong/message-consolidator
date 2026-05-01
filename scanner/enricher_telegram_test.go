package scanner

import (
	"fmt"
	"testing"
	"time"
)

func TestEnrichTelegramMessage_Fallback(t *testing.T) {
	t.Parallel()

	chatKey := "tg_user_123456789"
	msg := "Hello from TG"
	timestamp := time.Now()

	windowStart := (timestamp.Unix() / 900) * 900
	expectedThreadID := fmt.Sprintf("tg_thread_%s_%d", chatKey, windowStart)

	enriched, err := EnrichTelegramMessage(chatKey, msg, timestamp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enriched.SenderID != 0 {
		t.Errorf("SenderID = %d; want 0", enriched.SenderID)
	}
	if enriched.SenderName != chatKey {
		t.Errorf("SenderName = %q; want %q", enriched.SenderName, chatKey)
	}
	if enriched.SourceChannel != "telegram" {
		t.Errorf("SourceChannel = %q; want %q", enriched.SourceChannel, "telegram")
	}
	if enriched.VirtualThreadID != expectedThreadID {
		t.Errorf("VirtualThreadID = %q; want %q", enriched.VirtualThreadID, expectedThreadID)
	}
	if enriched.RawContent != msg {
		t.Errorf("RawContent = %q; want %q", enriched.RawContent, msg)
	}
}

func TestResolveTelegramSender_Fallback(t *testing.T) {
	t.Parallel()

	// Why: userCache is empty in test env; fallback path returns (0, chatKey).
	id, name := resolveTelegramSender("tg_user_123")
	if id != 0 {
		t.Errorf("id = %d; want 0", id)
	}
	if name != "tg_user_123" {
		t.Errorf("name = %q; want %q", name, "tg_user_123")
	}
}

func TestResolveTelegramSender_EmptyKey(t *testing.T) {
	t.Parallel()

	// Why: GetUserByTgID rejects empty input with an error; fallback returns (0, "").
	id, name := resolveTelegramSender("")
	if id != 0 {
		t.Errorf("id = %d; want 0", id)
	}
	if name != "" {
		t.Errorf("name = %q; want %q", name, "")
	}
}

func TestTelegramSenderShim(t *testing.T) {
	t.Parallel()

	id, name := telegramSenderShim("tg_user_xyz")
	if id != 0 {
		t.Errorf("id = %d; want 0", id)
	}
	if name != "tg_user_xyz" {
		t.Errorf("name = %q; want %q", name, "tg_user_xyz")
	}
}
